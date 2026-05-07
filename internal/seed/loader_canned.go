package seed

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"
	"gopkg.in/yaml.v3"
)

// cannedResponsesYAML mirrors the on-disk shape of seed/canned-responses-hand.yaml.
type cannedResponsesYAML struct {
	CannedLLM  []cannedLLMEntry  `yaml:"canned_llm"`
	CannedKapa []cannedKapaEntry `yaml:"canned_kapa"`
}

type cannedLLMEntry struct {
	TemplateName string         `yaml:"template_name"`
	Persona      string         `yaml:"persona"`
	Variant      string         `yaml:"variant"`
	Priority     int            `yaml:"priority"`
	RawJSON      map[string]any `yaml:"raw_json"`
}

type cannedKapaEntry struct {
	QueryPattern string         `yaml:"query_pattern"`
	ResponseJSON map[string]any `yaml:"response_json"`
}

// LoadCannedResponses parses seed/canned-responses-hand.yaml and upserts
// rows into both canned_responses (LLM) and canned_kapa_responses (Kapa).
// The composite key for canned_responses is (template_name, persona, variant)
// and the function uses ON CONFLICT to make the operation idempotent.
//
// Variant defaults to "default" if absent; priority defaults to 100.
func (s *Seeder) LoadCannedResponses(ctx context.Context) error {
	body, err := s.readFile("canned-responses-hand.yaml")
	if err != nil {
		return err
	}
	var doc cannedResponsesYAML
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return oops.Wrapf(err, "parse canned-responses-hand.yaml")
	}

	if err := upsertCannedLLM(ctx, s.pool, doc.CannedLLM); err != nil {
		return err
	}
	if err := upsertCannedKapa(ctx, s.pool, doc.CannedKapa); err != nil {
		return err
	}
	return nil
}

// upsertCannedLLM inserts (or updates on conflict) one row per canned LLM
// response. The unique constraint is (template_name, persona, variant); on
// conflict we update raw_json + priority (keep created_at).
func upsertCannedLLM(ctx context.Context, pool *pgxpool.Pool, entries []cannedLLMEntry) error {
	if len(entries) == 0 {
		return nil
	}
	const stmt = `
		INSERT INTO canned_responses (template_name, persona, variant, raw_json, priority)
		VALUES ($1, $2, $3, $4::jsonb, $5)
		ON CONFLICT (template_name, persona, variant)
		DO UPDATE SET raw_json = EXCLUDED.raw_json, priority = EXCLUDED.priority`

	for i, e := range entries {
		if e.TemplateName == "" || e.Persona == "" {
			return oops.With("index", i).Errorf("canned_llm: template_name and persona are required")
		}
		variant := e.Variant
		if variant == "" {
			variant = "default"
		}
		priority := e.Priority
		if priority == 0 {
			priority = 100
		}
		raw, err := json.Marshal(e.RawJSON)
		if err != nil {
			return oops.With("index", i).Wrapf(err, "marshal raw_json")
		}
		if _, err := pool.Exec(ctx, stmt, e.TemplateName, e.Persona, variant, raw, priority); err != nil {
			return oops.
				With("template", e.TemplateName).
				With("persona", e.Persona).
				With("variant", variant).
				Wrapf(err, "upsert canned_responses")
		}
	}
	return nil
}

// upsertCannedKapa inserts kapa rows. The schema does NOT have a unique
// constraint on query_pattern, so we delete-and-reinsert per pattern to
// keep the operation idempotent (see also: design §4.1).
func upsertCannedKapa(ctx context.Context, pool *pgxpool.Pool, entries []cannedKapaEntry) error {
	if len(entries) == 0 {
		return nil
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return oops.Wrapf(err, "begin canned_kapa tx")
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for i, e := range entries {
		if e.QueryPattern == "" {
			return oops.With("index", i).Errorf("canned_kapa: query_pattern is required")
		}
		raw, err := json.Marshal(e.ResponseJSON)
		if err != nil {
			return oops.With("index", i).Wrapf(err, "marshal response_json")
		}
		if _, err := tx.Exec(ctx,
			`DELETE FROM canned_kapa_responses WHERE query_pattern = $1`,
			e.QueryPattern,
		); err != nil {
			return oops.
				With("pattern", e.QueryPattern).
				Wrapf(err, "delete canned_kapa")
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO canned_kapa_responses (query_pattern, response_json)
			 VALUES ($1, $2::jsonb)`,
			e.QueryPattern, raw,
		); err != nil {
			return oops.
				With("pattern", e.QueryPattern).
				Wrapf(err, "insert canned_kapa")
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return oops.Wrapf(err, "commit canned_kapa tx")
	}
	return nil
}

// UpsertCannedLLMRow is exported so the live-refresh path can overwrite a
// single canned row after a successful local-agent capture without
// re-loading the entire YAML.
func (s *Seeder) UpsertCannedLLMRow(
	ctx context.Context,
	templateName, persona, variant string,
	priority int,
	rawJSON []byte,
) error {
	if templateName == "" || persona == "" {
		return oops.Errorf("UpsertCannedLLMRow: template_name and persona required")
	}
	if variant == "" {
		variant = "default"
	}
	if priority == 0 {
		priority = 100
	}
	const stmt = `
		INSERT INTO canned_responses (template_name, persona, variant, raw_json, priority)
		VALUES ($1, $2, $3, $4::jsonb, $5)
		ON CONFLICT (template_name, persona, variant)
		DO UPDATE SET raw_json = EXCLUDED.raw_json, priority = EXCLUDED.priority`
	_, err := s.pool.Exec(ctx, stmt, templateName, persona, variant, rawJSON, priority)
	if err != nil {
		return oops.Wrapf(err, "UpsertCannedLLMRow")
	}
	return nil
}

// UpsertCannedKapaRow is exported for the live-refresh path.
func (s *Seeder) UpsertCannedKapaRow(ctx context.Context, queryPattern string, responseJSON []byte) error {
	if queryPattern == "" {
		return oops.Errorf("UpsertCannedKapaRow: query_pattern required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return oops.Wrapf(err, "begin tx")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx,
		`DELETE FROM canned_kapa_responses WHERE query_pattern = $1`,
		queryPattern,
	); err != nil {
		return oops.Wrapf(err, "delete canned_kapa")
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO canned_kapa_responses (query_pattern, response_json)
		 VALUES ($1, $2::jsonb)`,
		queryPattern, responseJSON,
	); err != nil {
		return oops.Wrapf(err, "insert canned_kapa")
	}
	return tx.Commit(ctx)
}
