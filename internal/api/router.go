// Package api implements the chi-based HTTP API and SSE streaming endpoints
// for the realtime-ai-trigger service (§3.10 of the design).
//
// Construction is dependency-injected so tests can swap in stub Postgres
// pools and an isolated SSE hub. The Server type bundles the chi.Router with
// shared dependencies and a Handler() entry point for httptest and main.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/sse"
	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/window"
)

// SeedFS abstracts read access to the seed/ directory. Production code uses
// a real os-rooted FS (looking for /etc/seed/ then ./seed/); tests can pass
// an in-memory implementation. It deliberately does NOT use io/fs.FS to
// avoid pulling persona-config plumbing into too many packages — a single
// ReadFile method is enough for the wizard endpoint.
type SeedFS interface {
	// ReadFile returns the bytes for the given path relative to the seed
	// root (e.g. "persona-configs/realestate.yaml"). Returns os.ErrNotExist
	// when missing.
	ReadFile(path string) ([]byte, error)
}

// FireScriptFunc is the optional callback invoked by POST /api/demo/fire-script.
// Stage 1 ships a 501 stub; stage 3 wiring sets this to a real demo-fire
// invocation. The function returns the count of events sent.
//
// count specifies how many concurrent sessions to fire (1-3); speed is a
// playback multiplier (0.5, 1.0, or 2.0).
type FireScriptFunc func(ctx context.Context, persona string, count int, speed float64) (eventsSent int, err error)

// AdminSeedFunc is the optional callback invoked by POST /api/admin/seed.
// Stage 1 ships a 501 stub; the wired version calls seed.Seeder.LoadAll.
type AdminSeedFunc func(ctx context.Context) error

// DemoResetFunc is the optional callback invoked by POST /api/demo/reset after
// the Postgres state has been cleared. It purges any in-memory engine state
// (cooldown gate, window store) so the next demo run starts clean. Returns the
// number of cooldown entries and windows cleared.
type DemoResetFunc func(ctx context.Context) (cooldownsCleared, windowsCleared int, err error)

// EngineReloadFunc is the optional callback invoked by handleActivateConfig
// after persisting a custom config YAML. Calling it triggers an immediate rule
// reload so the new rules are live before the next 30-second periodic tick.
// Nil → skip (the periodic reloader will pick up changes eventually).
type EngineReloadFunc func(ctx context.Context) error

// Server wires the API. Construct via New, then mount via Handler().
type Server struct {
	pool        *pgxpool.Pool
	hub         *sse.Hub
	seed        SeedFS
	windowStore *window.Store
	router      *chi.Mux
	// metrics is the lightweight counter store used by /metrics. We keep
	// the Prometheus client lib out of the hackathon binary.
	metrics *metrics

	// fireScript is invoked by handleFireScript when set. Nil → 501 stub.
	fireScript FireScriptFunc

	// adminSeed is invoked by handleAdminSeed when set. Nil → 501 stub.
	adminSeed AdminSeedFunc

	// onDemoReset is invoked after the PG truncate in handleDemoReset. Nil → skip.
	onDemoReset DemoResetFunc

	// engineReloader is invoked after a successful activate-with-yaml so
	// the rules engine picks up the new rules immediately rather than
	// waiting for the 30-second periodic reload tick.
	engineReloader EngineReloadFunc

	// logger is used for structured server-side logging. Defaults to
	// slog.Default() when not set in Config.
	logger *slog.Logger
	// corsAllowedOrigins holds normalized entries from Config (trimmed, no trailing slash).
	corsAllowedOrigins []string

	// startedAt records process boot time for /metrics and /healthz.
	startedAt time.Time
}

// Config wires runtime options.
type Config struct {
	Pool           *pgxpool.Pool
	Hub            *sse.Hub
	Seed           SeedFS
	WindowStore    *window.Store
	FireScript     FireScriptFunc
	AdminSeed      AdminSeedFunc
	OnDemoReset    DemoResetFunc
	EngineReloader EngineReloadFunc
	// Logger is used for structured server-side logging. Nil → slog.Default().
	Logger *slog.Logger

	// CorsAllowedOrigins lists full browser Origins allowed in addition to localhost
	// dev URLs (e.g. https://app.example.com). Entries are normalized on New.
	CorsAllowedOrigins []string
}

// New builds a Server with the supplied dependencies and wires the chi
// router. Pool may be nil for tests that don't exercise DB-backed
// endpoints; in that case those endpoints return 503.
func New(cfg Config) *Server {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{
		pool:           cfg.Pool,
		hub:            cfg.Hub,
		seed:           cfg.Seed,
		windowStore:    cfg.WindowStore,
		fireScript:     cfg.FireScript,
		adminSeed:      cfg.AdminSeed,
		onDemoReset:    cfg.OnDemoReset,
		engineReloader: cfg.EngineReloader,
		logger:         logger,
		metrics:        newMetrics(),
		startedAt:      time.Now().UTC(),
	}
	for _, o := range cfg.CorsAllowedOrigins {
		if n := normalizeOrigin(o); n != "" {
			s.corsAllowedOrigins = append(s.corsAllowedOrigins, n)
		}
	}
	if s.hub == nil {
		s.hub = sse.NewHub()
	}
	s.router = s.buildRouter()
	return s
}

// Handler returns the http.Handler used by the main binary or by httptest.
func (s *Server) Handler() http.Handler { return s.router }

// Hub exposes the underlying SSE hub for the consumer/dispatcher path
// (so they can publish without re-constructing it).
func (s *Server) Hub() *sse.Hub { return s.hub }

// buildRouter mounts every endpoint per §3.10.
func (s *Server) buildRouter() *chi.Mux {
	r := chi.NewRouter()

	// Stable middleware. RequestID + Recoverer are universally useful;
	// Timeout is moderate so SSE handlers (which never return until the
	// client disconnects) bypass it via their own context handling.
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(s.metricsMiddleware)
	// CORS: localhost dev origins + optional Config.CorsAllowedOrigins (env-driven in prod).
	r.Use(s.corsMiddleware)

	// Health / readiness / metrics — no /api prefix.
	r.Get("/healthz", s.handleHealthz)
	r.Get("/readyz", s.handleReadyz)
	r.Get("/metrics", s.handleMetrics)

	r.Route("/api", func(r chi.Router) {
		// Tracking plan (read from `tracking_plans` JSONB).
		r.Get("/tracking-plan/{persona}", s.handleGetTrackingPlan)

		// Onboarding wizard.
		r.Post("/onboarding/generate-config", s.handleGenerateConfig)
		r.Post("/onboarding/activate", s.handleActivateConfig)

		// SSE streams.
		r.Get("/streams/{stream}", s.handleSSEStream)

		// Demo controller.
		r.Post("/demo/fire-script", s.handleFireScript)
		r.Post("/demo/reset", s.handleDemoReset)
		r.Post("/demo/replay-last-trigger", s.handleReplayLastTrigger)

		// Mock email viewer.
		r.Get("/mock-emails", s.handleListMockEmails)

		// Dashboard rehydration endpoints (browser refresh / initial load).
		r.Get("/recent-events", s.handleRecentEvents)
		r.Get("/active-sessions", s.handleActiveSessions)
		r.Get("/recent-triggers", s.handleRecentTriggers)

		// Admin.
		r.Post("/admin/seed", s.handleAdminSeed)
		r.Get("/admin/canned", s.handleAdminCanned)
	})

	return r
}

func normalizeOrigin(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "/")
	return s
}

func (s *Server) originAllowed(origin string) bool {
	if origin == "" {
		return false
	}
	// Local dev: always allowed, independent of CorsAllowedOrigins / CORS_ALLOWED_ORIGINS.
	if strings.HasPrefix(origin, "http://localhost:") ||
		strings.HasPrefix(origin, "http://127.0.0.1:") ||
		strings.HasPrefix(origin, "https://localhost:") ||
		strings.HasPrefix(origin, "https://127.0.0.1:") {
		return true
	}
	if origin == "http://localhost" || origin == "https://localhost" ||
		origin == "http://127.0.0.1" || origin == "https://127.0.0.1" {
		return true
	}
	n := normalizeOrigin(origin)
	for _, allowed := range s.corsAllowedOrigins {
		if n == allowed {
			return true
		}
	}
	return false
}

// corsMiddleware reflects allowed Origins for JSON and SSE. Localhost and
// loopback on any port are always allowed; CorsAllowedOrigins adds deployed
// frontends (normalized exact match).
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if s.originAllowed(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Cache-Control, X-Requested-With")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Max-Age", "300")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
