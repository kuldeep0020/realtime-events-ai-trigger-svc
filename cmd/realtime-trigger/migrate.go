package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/db"
)

// runMigrate applies all pending goose migrations against POSTGRES_DSN.
//
// Flags:
//
//	--dsn               override POSTGRES_DSN
//	--migrations-dir    path to *.sql files (default: ./migrations)
func runMigrate(args []string) {
	fs := flag.NewFlagSet("migrate", flag.ExitOnError)
	dsn := fs.String("dsn", os.Getenv("POSTGRES_DSN"), "Postgres DSN (defaults to POSTGRES_DSN env)")
	migrationsDir := fs.String("migrations-dir", defaultMigrationsDir(), "directory containing *.sql migrations")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: parse flags: %v\n", err)
		os.Exit(2)
	}

	if *dsn == "" {
		fmt.Fprintln(os.Stderr, "migrate: --dsn or POSTGRES_DSN env is required")
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(ctx, *dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "migrate: open pool: %v\n", err)
		os.Exit(1)
	}
	defer db.Close(pool)

	if err := db.RunMigrationsUp(ctx, pool, *migrationsDir); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: run migrations: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "migrate: applied migrations from "+*migrationsDir)
}

// defaultMigrationsDir picks ./migrations when present (local dev), otherwise
// /etc/migrations (in-cluster mount). Falls back to ./migrations.
func defaultMigrationsDir() string {
	candidates := []string{"./migrations", "/etc/migrations"}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			return c
		}
	}
	return "./migrations"
}
