package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/api"
	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/consumer"
	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/db"
)

// runServe boots the full service: Pulsar consumer → filter → window →
// rules → dispatcher, plus the chi HTTP API on :8080. Lifecycle is managed
// by an errgroup; SIGINT/SIGTERM cancel the root context, and every
// component honours the cancellation.
func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	skipPulsar := fs.Bool("skip-pulsar", false, "skip Pulsar consumer (for local dev with no broker)")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "serve: parse flags: %v\n", err)
		os.Exit(2)
	}

	cfg, err := loadRuntimeConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "serve: config: %v\n", err)
		os.Exit(2)
	}

	log := newLogger(cfg.LogLevel)
	log.Info("serve: starting",
		"http_addr", cfg.HTTPAddr,
		"llm_mode", cfg.LLMMode,
		"kapa_mode", cfg.KapaMode,
		"activation_mode", cfg.ActivationMode,
		"skip_pulsar", *skipPulsar,
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(ctx, cfg.PostgresDSN)
	if err != nil {
		fmt.Fprintf(os.Stderr, "serve: db open: %v\n", err)
		os.Exit(1)
	}
	defer db.Close(pool)

	rt, err := buildRuntime(ctx, cfg, pool, log)
	if err != nil {
		fmt.Fprintf(os.Stderr, "serve: build runtime: %v\n", err)
		os.Exit(1)
	}

	// Build the chi server — wired to the seed FS, the SSE hub, and the
	// admin/seed + fire-script callbacks.
	seedFS := api.NewDiskSeedFS()
	apiSrv := api.New(api.Config{
		Pool:           pool,
		Hub:            rt.hub,
		Seed:           seedFS,
		WindowStore:    rt.windows,
		FireScript:     rt.fireScriptHandler,
		AdminSeed:      rt.adminSeedHandler(seedFS),
		OnDemoReset:    rt.OnDemoReset,
		EngineReloader: rt.engine.Reload,
		Logger:         log,
	})

	httpSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           apiSrv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	g, gCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		log.Info("serve: HTTP listening", "addr", cfg.HTTPAddr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	})

	if !*skipPulsar {
		g.Go(func() error {
			cons, err := consumer.New(gCtx, consumer.Config{
				URL:                 cfg.PulsarURL,
				Token:               cfg.PulsarToken,
				Topic:               cfg.PulsarTopic,
				SubscriptionName:    cfg.PulsarSubscription,
				TLSTrustCertsFile:   cfg.PulsarTLSTrustCerts,
				TLSValidateHostname: cfg.PulsarTLSValidateHostname,
				TLSAllowInsecure:    cfg.PulsarTLSAllowInsecure,
			}, rt.consumerOut, log)
			if err != nil {
				return fmt.Errorf("pulsar consumer: %w", err)
			}
			defer cons.Close()
			cons.Run(gCtx)
			return nil
		})
	} else {
		log.Warn("serve: Pulsar consumer skipped (--skip-pulsar)")
	}

	g.Go(func() error { rt.runPipeline(gCtx); return nil })
	g.Go(func() error { rt.windows.RunPruner(gCtx, 15*time.Minute, time.Minute); return nil })
	g.Go(func() error { rt.fanoutPrunes(gCtx); return nil })
	g.Go(func() error { rt.runArchive(gCtx); return nil })
	g.Go(func() error { rt.runIdleTicker(gCtx); return nil })
	g.Go(func() error { rt.runDispatcher(gCtx); return nil })

	g.Go(func() error {
		<-gCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		log.Info("serve: shutting down")
		_ = httpSrv.Shutdown(shutdownCtx)
		_ = rt.hub.Close(shutdownCtx)
		return nil
	})

	if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("serve: exiting on error", "err", err)
		os.Exit(1)
	}
	log.Info("serve: stopped cleanly")
}
