// Package seed loads hand-curated YAML/JSON content from the seed/ tree
// (mock_profiles, persona-configs, action-templates, tracking_plans, and
// canned_responses) and upserts it into Postgres.
//
// Two flows are supported:
//
//   - LoadAll: idempotent upsert of every YAML/JSON file in the seed FS.
//   - RefreshLive: re-fetches LLM and Kapa canned responses by calling the
//     live local-agent and Kapa APIs, then overwrites the canned_* rows.
//
// The seeder is deliberately split per-loader file so each loader is small
// (<100 lines) and independently testable. All loaders are invoked under a
// single Postgres pool but each opens its own short-lived transaction to
// keep failures isolated.
package seed

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"

	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/api"
)

// SeedFS is the read-only file abstraction. We re-export the api package's
// interface so callers don't have to import api just for SeedFS — they pass
// it in via Seeder construction.
type SeedFS = api.SeedFS

// Seeder bundles the pgxpool and a SeedFS so loaders can share connection
// state without being passed both arguments.
type Seeder struct {
	pool *pgxpool.Pool
	fs   SeedFS
}

// NewSeeder constructs a Seeder. Both pool and fs are required; LoadAll
// returns a clear error if either is nil.
func NewSeeder(pool *pgxpool.Pool, fs SeedFS) *Seeder {
	return &Seeder{pool: pool, fs: fs}
}

// LoadAll runs every loader in deterministic order. Loader-level errors are
// wrapped with context and returned immediately — partial state may have
// been committed by earlier loaders, which is acceptable because every
// loader is idempotent.
//
// Order matters for foreign-key sequencing:
//
//  1. action_templates    (no FKs)
//  2. mock_profiles       (no FKs)
//  3. tracking_plans      (no FKs)
//  4. configs + rules     (rules.config_id → configs.id)
//  5. canned_responses    (no FKs)
//  6. canned_kapa         (no FKs)
func (s *Seeder) LoadAll(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return oops.Errorf("seed: pool is nil")
	}
	if s.fs == nil {
		return oops.Errorf("seed: filesystem is nil")
	}

	steps := []struct {
		name string
		fn   func(context.Context) error
	}{
		{"action_templates", s.LoadActionTemplates},
		{"mock_profiles", s.LoadMockProfiles},
		{"tracking_plans", s.LoadTrackingPlans},
		{"persona_configs", s.LoadPersonaConfigs},
		{"canned_responses", s.LoadCannedResponses},
	}
	for _, step := range steps {
		if err := step.fn(ctx); err != nil {
			return oops.Wrapf(err, "seed: %s", step.name)
		}
	}
	return nil
}

// readFile is a small helper that wraps fs.ReadFile with a clear error
// message when the path is missing.
func (s *Seeder) readFile(path string) ([]byte, error) {
	body, err := s.fs.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return body, nil
}
