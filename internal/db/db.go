package db

import (
	"context"
	"database/sql"
	"embed"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/samber/oops"
)

const (
	maxConns        = 16
	acquireTimeout  = 30 * time.Second
)

// Open creates and validates a pgxpool connection pool.
// dsn should be a standard PostgreSQL connection string or URL.
func Open(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, oops.Wrapf(err, "parsing postgres DSN")
	}

	cfg.MaxConns = maxConns
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = 30 * time.Second

	// ConnectTimeout is on the underlying pgconn.Config (per-connection dial timeout).
	cfg.ConnConfig.Config.ConnectTimeout = acquireTimeout

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, oops.Wrapf(err, "creating pgxpool")
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, oops.Wrapf(err, "pinging postgres")
	}

	return pool, nil
}

// Close drains and closes the connection pool.
func Close(pool *pgxpool.Pool) {
	if pool != nil {
		pool.Close()
	}
}

// migrationsFS is populated by the caller via SetMigrationsFS when embedding
// is not available. If nil, goose uses the filesystem path directly.
var migrationsFS *embed.FS

// SetMigrationsFS allows callers (e.g., main) to inject the embedded FS produced
// by go:embed. Not required when migrationsDir points to a real filesystem path.
func SetMigrationsFS(fs *embed.FS) {
	migrationsFS = fs
}

// RunMigrationsUp runs all pending goose migrations against the pool.
// migrationsDir is the path (relative or absolute) to the *.sql files when
// migrationsFS is nil. When migrationsFS is set, migrationsDir is the embed
// sub-path within that FS.
func RunMigrationsUp(ctx context.Context, pool *pgxpool.Pool, migrationsDir string) error {
	db := stdlib.OpenDBFromPool(pool)
	defer func() { _ = db.Close() }()

	if err := runMigrations(ctx, db, migrationsDir); err != nil {
		return oops.Wrapf(err, "running goose migrations up")
	}
	return nil
}

func runMigrations(ctx context.Context, db *sql.DB, migrationsDir string) error {
	goose.SetBaseFS(nil)

	if migrationsFS != nil {
		goose.SetBaseFS(migrationsFS)
	}

	if err := goose.SetDialect("postgres"); err != nil {
		return oops.Wrapf(err, "setting goose dialect")
	}

	if err := goose.UpContext(ctx, db, migrationsDir); err != nil {
		return oops.Wrapf(err, "goose up")
	}
	return nil
}
