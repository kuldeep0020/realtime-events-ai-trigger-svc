package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/api"
	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/db"
	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/kapa"
	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/llm"
	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/seed"
)

// runSeed invokes the seed loader against POSTGRES_DSN.
//
// `--from hand` (default) reads YAML/JSON from the seed/ tree and upserts
// every loader's rows. `--from live` first runs the hand seed, then refreshes
// the canned LLM and Kapa rows by calling the live local-agent and Kapa APIs.
//
// Flags:
//
//	--from              "hand" (default) | "live"
//	--dsn               override POSTGRES_DSN
//	--seed-dir          override seed root (default: ./seed)
//	--persona           limit live refresh to one persona (optional)
//	--skip-kapa         skip Kapa refresh in live mode (e.g. when KAPA_API_KEY missing)
func runSeed(args []string) {
	fs := flag.NewFlagSet("seed", flag.ExitOnError)
	from := fs.String("from", "hand", "seed source: 'hand' (YAML files) or 'live' (call APIs)")
	dsn := fs.String("dsn", os.Getenv("POSTGRES_DSN"), "Postgres DSN (defaults to POSTGRES_DSN env)")
	seedDir := fs.String("seed-dir", "", "override seed root (default: /etc/seed then ./seed)")
	personaFilter := fs.String("persona", "", "live mode: limit refresh to one persona ('realestate' or 'rs-self')")
	skipKapa := fs.Bool("skip-kapa", false, "live mode: skip Kapa refresh (use when KAPA_API_KEY/KAPA_PROJECT_ID unset)")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "seed: parse flags: %v\n", err)
		os.Exit(2)
	}

	if *dsn == "" {
		fmt.Fprintln(os.Stderr, "seed: --dsn or POSTGRES_DSN env is required")
		os.Exit(2)
	}
	if *from != "hand" && *from != "live" {
		fmt.Fprintf(os.Stderr, "seed: unknown --from %q (expected 'hand' or 'live')\n", *from)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(ctx, *dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "seed: open pool: %v\n", err)
		os.Exit(1)
	}
	defer db.Close(pool)

	seedFS := newSeedFS(*seedDir)
	seeder := seed.NewSeeder(pool, seedFS)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Phase 1: idempotent hand seed.
	if err := seeder.LoadAll(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "seed: load hand: %v\n", err)
		os.Exit(1)
	}
	log.Info("seed: hand load complete")

	if *from == "hand" {
		fmt.Fprintln(os.Stderr, "seed: hand seed complete")
		return
	}

	// Phase 2: live refresh — overwrites canned_responses and (optionally)
	// canned_kapa_responses by hitting the live APIs.
	if err := refreshLiveLLM(ctx, seeder, log, *personaFilter); err != nil {
		fmt.Fprintf(os.Stderr, "seed: refresh live LLM: %v\n", err)
		os.Exit(1)
	}
	if !*skipKapa {
		if err := refreshLiveKapa(ctx, seeder, log); err != nil {
			fmt.Fprintf(os.Stderr, "seed: refresh live kapa: %v\n", err)
			os.Exit(1)
		}
	} else {
		log.Info("seed: skipping Kapa refresh (--skip-kapa)")
	}
	fmt.Fprintln(os.Stderr, "seed: live refresh complete")
}

// refreshLiveLLM constructs a LocalAgentClient from env and refreshes every
// (template, persona) target. Personas can be filtered via --persona.
func refreshLiveLLM(ctx context.Context, seeder *seed.Seeder, log *slog.Logger, personaFilter string) error {
	client, err := llm.NewLocalAgentClient(llm.LocalAgentConfig{
		URL:    os.Getenv("LOCAL_AGENT_URL"),
		Bearer: os.Getenv("LOCAL_AGENT_TOKEN"),
		Model:  os.Getenv("LOCAL_AGENT_MODEL"),
	})
	if err != nil {
		return fmt.Errorf("local agent: %w", err)
	}

	targets := seed.DefaultRefreshTargets
	if personaFilter != "" {
		filtered := make([]seed.RefreshTarget, 0, len(targets))
		for _, t := range targets {
			if t.Persona == personaFilter {
				filtered = append(filtered, t)
			}
		}
		targets = filtered
		if len(targets) == 0 {
			return fmt.Errorf("no targets match persona %q", personaFilter)
		}
	}

	// Build minimal vars per persona. The hackathon seed run intentionally
	// uses placeholder context — the goal is to capture a high-quality
	// canned response for the demo, not to render real visitor data here.
	varsByPersona := map[string]llm.TemplateVars{
		llm.PersonaRealestate: {
			Persona:            llm.PersonaRealestate,
			AnonymousID:        "anon_demo-re-001",
			WindowSnapshotJSON: `{"event_count":4,"path_latest":"/listings/L112"}`,
			FullEventsJSON:     `[]`,
			TraitsJSON:         `{"membership_tier":"browse"}`,
			RealtorRosterJSON:  `[{"name":"Priya N.","suburbs":["suburb-1","suburb-2"]}]`,
		},
		llm.PersonaRSSelf: {
			Persona:            llm.PersonaRSSelf,
			UserID:             "demo-rs-001",
			AnonymousID:        "demo-rs-001",
			WindowSnapshotJSON: `{"event_count":4,"has_error_event":true}`,
			FullEventsJSON:     `[]`,
			TraitsJSON:         `{"plan":"free"}`,
			KapaResultsJSON:    `{"answer":"...","is_uncertain":false}`,
			LastErrorEventJSON: `{"eventName":"Destination Setup Error","properties":{"error_code":"AMP_INVALID_API_KEY"}}`,
		},
	}

	count, err := seeder.RefreshLiveLLM(ctx, client, varsByPersona, targets, log)
	if err != nil {
		return err
	}
	log.Info("seed: live LLM refresh", "rows_updated", count)
	return nil
}

// refreshLiveKapa constructs a Kapa LiveClient from env and refreshes the
// canned-kapa entries.
func refreshLiveKapa(ctx context.Context, seeder *seed.Seeder, log *slog.Logger) error {
	client, err := kapa.NewLiveClient(kapa.LiveConfig{
		ProjectID: os.Getenv("KAPA_PROJECT_ID"),
		APIKey:    os.Getenv("KAPA_API_KEY"),
	})
	if err != nil {
		return fmt.Errorf("kapa client: %w", err)
	}
	count, err := seeder.RefreshLiveKapa(ctx, client, nil, log)
	if err != nil {
		return err
	}
	log.Info("seed: live Kapa refresh", "rows_updated", count)
	return nil
}

// newSeedFS picks a SeedFS rooted at --seed-dir if non-empty, else default.
func newSeedFS(override string) seed.SeedFS {
	if override != "" {
		return api.NewDiskSeedFS(override)
	}
	return api.NewDiskSeedFS()
}
