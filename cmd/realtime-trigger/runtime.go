package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/activation"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/api"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/consumer"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/demofire"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/dispatch"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/filter"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/kapa"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/llm"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/rules"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/seed"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/sse"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/window"
)

// dispatchItem wraps a Match with its resolved persona for the async
// worker pool. The persona is resolved synchronously on the hot path so the
// worker doesn't need access to the engine.
type dispatchItem struct {
	m       rules.Match
	persona string
}

// runtime bundles every wired component for the serve mode.
type runtime struct {
	cfg         runtimeConfig
	pool        *pgxpool.Pool
	hub         *sse.Hub
	log         *slog.Logger
	allowedKeys map[string]bool

	// pipeline channels
	consumerOut chan consumer.ProcessedEvent
	archiveCh   chan consumer.ProcessedEvent

	// matchCh decouples rule evaluation (hot path) from the blocking
	// fireMatch work (LLM + Slack + PG). Buffer of 64 so short bursts are
	// absorbed; drops above the watermark are logged as match_dropped.
	matchCh chan dispatchItem

	// matchDropped counts items dropped from matchCh due to back-pressure.
	matchDropped atomic.Int64

	// pgCooldownOverrides counts times a PG cooldown row suppressed a fire
	// that the in-memory engine gate had already allowed. Useful for diagnosing
	// post-restart demo runs where PG rows survive but in-memory state was lost.
	pgCooldownOverrides atomic.Int64

	// stages
	flt        *filter.Filter
	windows    *window.Store
	engine     *rules.Engine
	dispatcher *dispatch.Dispatcher

	// clients (mode-selected)
	llmClient        llm.Client
	kapaClient       kapa.Client
	activationClient activation.Client

	// demo-fire & admin-seed handlers wired into the API
	fireScriptHandler api.FireScriptFunc
	adminSeedFn       func(ctx context.Context, fs api.SeedFS) error
}

func (rt *runtime) adminSeedHandler(fs api.SeedFS) api.AdminSeedFunc {
	return func(ctx context.Context) error { return rt.adminSeedFn(ctx, fs) }
}

// OnDemoReset purges all in-memory state that the PG DELETE statements in
// handleDemoReset cannot reach: the MemoryCooldownGate entries and every
// UserWindow in the sharded store. Both are required so a second demo run
// after a reset produces identical trigger fires.
//
// The returned counts are forwarded to the HTTP response for operator visibility.
func (rt *runtime) OnDemoReset(_ context.Context) (cooldownsCleared, windowsCleared int, err error) {
	cooldownsCleared = rt.engine.PurgeCooldowns()
	windowsCleared = rt.windows.Reset()
	rt.log.Info("demo reset: in-memory state purged",
		"cooldowns_cleared", cooldownsCleared,
		"windows_cleared", windowsCleared)
	return cooldownsCleared, windowsCleared, nil
}

// buildRuntime wires every component but does NOT start any goroutines.
// runServe owns goroutine lifecycle via the errgroup.
func buildRuntime(ctx context.Context, cfg runtimeConfig, pool *pgxpool.Pool, log *slog.Logger) (*runtime, error) {
	allowed := make(map[string]bool, len(cfg.AllowedWriteKeys))
	for _, k := range cfg.AllowedWriteKeys {
		allowed[k] = true
	}

	hub := sse.NewHub()
	flt := filter.New(filter.Config{
		AllowedWriteKeys: allowed,
		RedactPaths: []string{
			"properties.email",
			"properties.phone",
			"traits.email",
			"traits.phone",
			"context.traits.email",
			"context.traits.phone",
		},
	})

	windows := window.New(0)
	engine := rules.NewEngine(nil, rules.NewMemoryCooldownGate())

	llmClient, err := buildLLMClient(cfg, pool, log)
	if err != nil {
		return nil, fmt.Errorf("llm client: %w", err)
	}
	kapaClient, err := buildKapaClient(cfg, pool, log)
	if err != nil {
		return nil, fmt.Errorf("kapa client: %w", err)
	}
	activationClient, err := buildActivationClient(cfg, pool, log)
	if err != nil {
		return nil, fmt.Errorf("activation client: %w", err)
	}

	dispatcher := dispatch.New()
	if cfg.SlackWebhookURL != "" {
		dispatcher.Register("slack", dispatch.NewSlackBackend(cfg.SlackWebhookURL))
	} else {
		log.Warn("serve: SLACK_WEBHOOK_URL unset — slack dispatch will fail")
	}
	dispatcher.Register("email", dispatch.NewMockEmailBackend(pool, "demo@rudderstack.com"))

	rt := &runtime{
		cfg:              cfg,
		pool:             pool,
		hub:              hub,
		log:              log,
		allowedKeys:      allowed,
		consumerOut:      make(chan consumer.ProcessedEvent, 1024),
		archiveCh:        make(chan consumer.ProcessedEvent, 1024),
		matchCh:          make(chan dispatchItem, 64),
		flt:              flt,
		windows:          windows,
		engine:           engine,
		dispatcher:       dispatcher,
		llmClient:        llmClient,
		kapaClient:       kapaClient,
		activationClient: activationClient,
	}

	engine.SetLoader(rt.loadRulesFromPG)
	if err := engine.Reload(ctx); err != nil {
		log.Warn("serve: initial rule reload failed (proceeding with empty rules)", "err", err)
	}
	engine.RunReloader(ctx, 30*time.Second)

	rt.fireScriptHandler = rt.makeFireScript()
	rt.adminSeedFn = rt.makeAdminSeed()
	return rt, nil
}

// makeFireScript wraps the appropriate demofire backend into the
// api.FireScriptFunc signature expected by the API server.
//
// When cfg.DemoFireTarget == "pulsar" (the default), events are published
// directly to the local Pulsar broker so that the consumer running in the
// same process receives them. When cfg.DemoFireTarget == "http", the legacy
// HTTP path (POST to the ingestion-svc URL) is used instead.
func (rt *runtime) makeFireScript() api.FireScriptFunc {
	return func(ctx context.Context, persona string) (int, error) {
		script := demofire.ScriptForPersona(persona)
		if script == nil {
			return 0, fmt.Errorf("unknown persona %q", persona)
		}

		wk := demofire.PersonaWriteKey(persona)
		if wk == "" && len(rt.cfg.AllowedWriteKeys) > 0 {
			wk = rt.cfg.AllowedWriteKeys[0]
		}

		switch rt.cfg.DemoFireTarget {
		case "http":
			if rt.cfg.IngestionURL == "" {
				return 0, fmt.Errorf("fire-script: INGESTION_URL is required when DEMO_FIRE_TARGET=http")
			}
			if wk == "" {
				return 0, fmt.Errorf("fire-script: no write key available for persona %q", persona)
			}
			firer := demofire.NewFirer(rt.cfg.IngestionURL, wk)
			firer.Logger = rt.log
			return firer.Fire(ctx, script)

		default: // "pulsar"
			if wk == "" {
				return 0, fmt.Errorf("fire-script: no write key available for persona %q", persona)
			}
			pf := demofire.NewPulsarFirer(demofire.PulsarFirerConfig{
				URL:                 rt.cfg.PulsarURL,
				Topic:               rt.cfg.PulsarTopic,
				Token:               rt.cfg.PulsarToken,
				TLSTrustCertsFile:   rt.cfg.PulsarTLSTrustCerts,
				TLSValidateHostname: rt.cfg.PulsarTLSValidateHostname,
				WriteKey:            wk,
				SourceID:            "",
			})
			pf.Logger = rt.log
			return pf.Fire(ctx, script)
		}
	}
}

// makeAdminSeed returns a function that re-runs the seed loader with the
// supplied SeedFS. We capture rt.pool by closure so the API server doesn't
// need to construct another Seeder.
func (rt *runtime) makeAdminSeed() func(ctx context.Context, fs api.SeedFS) error {
	return func(ctx context.Context, fs api.SeedFS) error {
		seeder := seed.NewSeeder(rt.pool, fs)
		return seeder.LoadAll(ctx)
	}
}

// --- client builders --------------------------------------------------------

func buildLLMClient(cfg runtimeConfig, pool *pgxpool.Pool, log *slog.Logger) (llm.Client, error) {
	switch strings.ToLower(cfg.LLMMode) {
	case "canned", "":
		return llm.NewCannedClient(pool), nil
	case "live":
		c, err := llm.NewLocalAgentClient(llm.LocalAgentConfig{
			URL:    os.Getenv("LOCAL_AGENT_URL"),
			Bearer: os.Getenv("LOCAL_AGENT_TOKEN"),
			Model:  os.Getenv("LOCAL_AGENT_MODEL"),
		})
		if err != nil {
			log.Warn("serve: local agent client init failed — falling back to canned", "err", err)
			return llm.NewCannedClient(pool), nil
		}
		return c, nil
	default:
		return nil, fmt.Errorf("unknown LLM_MODE %q", cfg.LLMMode)
	}
}

func buildKapaClient(cfg runtimeConfig, pool *pgxpool.Pool, log *slog.Logger) (kapa.Client, error) {
	switch strings.ToLower(cfg.KapaMode) {
	case "canned", "":
		return kapa.NewCannedClient(pool), nil
	case "live":
		c, err := kapa.NewLiveClient(kapa.LiveConfig{
			ProjectID: os.Getenv("KAPA_PROJECT_ID"),
			APIKey:    os.Getenv("KAPA_API_KEY"),
		})
		if err != nil {
			log.Warn("serve: kapa live client init failed — falling back to canned", "err", err)
			return kapa.NewCannedClient(pool), nil
		}
		return c, nil
	default:
		return nil, fmt.Errorf("unknown KAPA_MODE %q", cfg.KapaMode)
	}
}

func buildActivationClient(cfg runtimeConfig, pool *pgxpool.Pool, log *slog.Logger) (activation.Client, error) {
	switch strings.ToLower(cfg.ActivationMode) {
	case "mock", "":
		return activation.NewMockClient(pool), nil
	case "live":
		log.Warn("serve: ACTIVATION_MODE=live not implemented in WP-D — falling back to mock")
		return activation.NewMockClient(pool), nil
	default:
		return nil, fmt.Errorf("unknown ACTIVATION_MODE %q", cfg.ActivationMode)
	}
}

// newLogger builds a slog.Logger honouring LOG_LEVEL.
func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
}
