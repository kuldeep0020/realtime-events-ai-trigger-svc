package seed

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/samber/oops"
	"gopkg.in/yaml.v3"
)

// personaConfigOnDisk mirrors the YAML shape of seed/persona-configs/*.yaml.
// We re-declare the shape locally rather than importing internal/rules to
// keep this loader independent of rule compilation — we only need the raw
// rule body to write into rules.spec JSONB.
type personaConfigOnDisk struct {
	Persona      string                   `yaml:"persona"`
	SlackChannel string                   `yaml:"slack_channel,omitempty"`
	Realtors     []map[string]any         `yaml:"realtors,omitempty"`
	Rules        []map[string]any         `yaml:"rules"`
}

// personaConfigPaths lists the on-disk persona configs we ship with the
// hackathon. New personas → add a row here; the loader iterates this list.
var personaConfigPaths = []struct {
	Persona string
	Path    string
}{
	{"realestate", "persona-configs/realestate.yaml"},
	{"rs-self", "persona-configs/rs-self.yaml"},
}

// LoadPersonaConfigs parses each persona's YAML config into:
//
//   - one row in `configs` (persona, config_yaml=raw bytes, active=true)
//   - one row per rule in `rules` (config_id FK, name, spec JSONB)
//
// The function uses a transaction to ensure either all rows for a persona
// land or none do. Re-running is idempotent: we delete + reinsert per
// persona because configs has no unique constraint we can ON CONFLICT on
// without altering schema.
func (s *Seeder) LoadPersonaConfigs(ctx context.Context) error {
	for _, p := range personaConfigPaths {
		body, err := s.readFile(p.Path)
		if err != nil {
			return err
		}
		var cfg personaConfigOnDisk
		if err := yaml.Unmarshal(body, &cfg); err != nil {
			return oops.With("path", p.Path).Wrapf(err, "parse persona config")
		}
		if cfg.Persona == "" {
			cfg.Persona = p.Persona
		}
		if cfg.Persona != p.Persona {
			return oops.
				With("path", p.Path).
				With("expected", p.Persona).
				With("actual", cfg.Persona).
				Errorf("persona mismatch in YAML body")
		}
		if err := s.upsertPersonaConfig(ctx, cfg, body); err != nil {
			return oops.With("persona", cfg.Persona).Wrap(err)
		}
	}
	return nil
}

// upsertPersonaConfig writes a single persona config + its rules atomically.
// We delete any prior config for (tenant_id="default", persona) so the seed
// is idempotent without relying on a unique constraint.
func (s *Seeder) upsertPersonaConfig(ctx context.Context, cfg personaConfigOnDisk, rawYAML []byte) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return oops.Wrapf(err, "begin tx")
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const tenantID = "default"

	// Remove any prior configs (and their rules — ON DELETE CASCADE).
	if _, err := tx.Exec(ctx,
		`DELETE FROM configs WHERE tenant_id = $1 AND persona = $2`,
		tenantID, cfg.Persona,
	); err != nil {
		return oops.Wrapf(err, "delete prior configs")
	}

	// Insert new config row.
	var configID uuid.UUID
	err = tx.QueryRow(ctx,
		`INSERT INTO configs (tenant_id, persona, config_yaml, active)
		 VALUES ($1, $2, $3, TRUE)
		 RETURNING id`,
		tenantID, cfg.Persona, string(rawYAML),
	).Scan(&configID)
	if err != nil {
		return oops.Wrapf(err, "insert config")
	}

	// Insert rules.
	for i, r := range cfg.Rules {
		name, _ := r["name"].(string)
		if name == "" {
			return oops.With("index", i).Errorf("rule missing name")
		}
		// We marshal the entire rule map (including name + when + fire) as
		// the spec JSONB. Downstream loaders re-parse this via
		// rules.LoadPersonaConfigJSON when constructing the engine state.
		// To match what loader.go expects we wrap rules in a stub config
		// envelope and persist that — but rules.spec is per-rule JSONB,
		// so we store ONLY the rule body (when + fire) plus name.
		spec, err := json.Marshal(r)
		if err != nil {
			return oops.With("rule", name).Wrapf(err, "marshal rule spec")
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO rules (config_id, name, spec, enabled)
			 VALUES ($1, $2, $3::jsonb, TRUE)`,
			configID, name, spec,
		); err != nil {
			return oops.With("rule", name).Wrapf(err, "insert rule")
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return oops.Wrapf(err, "commit")
	}
	return nil
}

// trackingPlanPath returns the seed-relative path for a persona's tracking
// plan JSON.
func trackingPlanPath(persona string) string {
	return fmt.Sprintf("tracking_plans/%s.json", persona)
}
