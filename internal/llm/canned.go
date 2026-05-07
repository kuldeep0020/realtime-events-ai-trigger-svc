package llm

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/db"
	"github.com/samber/oops"
)

// CannedClient reads pre-rendered action JSON from Postgres
// `canned_responses`, looking up by (template_name, persona) and using the
// highest-priority row.
//
// Two-layer safety net (§3.7):
//
//  1. PG row matched by (template_name, persona) — Source="canned"
//  2. Hardcoded default compiled into the binary — Source="fallback",
//     used when the table is empty or the row's JSON is unparseable.
//
// The demo path MUST always produce something renderable, so a CannedClient
// constructed with a nil pool degrades to the hardcoded layer rather than
// erroring.
type CannedClient struct {
	pool *pgxpool.Pool
}

// NewCannedClient builds a CannedClient backed by an existing pool. A nil
// pool is allowed (degrades all calls to hardcoded defaults).
func NewCannedClient(pool *pgxpool.Pool) *CannedClient {
	return &CannedClient{pool: pool}
}

// Generate looks up the canned action for (templateName, persona). On any
// miss (no rows, nil pool, decode error) it returns the hardcoded default
// with Source="fallback" so the demo never produces empty output.
func (c *CannedClient) Generate(ctx context.Context, templateName string, vars TemplateVars) (ActionResult, error) {
	if c == nil || c.pool == nil {
		return hardcodedDefault(templateName, vars, "pool unavailable"), nil
	}

	raw, err := db.LoadCannedResponse(ctx, c.pool, templateName, vars.Persona)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return hardcodedDefault(templateName, vars, "no canned row for (template,persona)"), nil
		}
		return ActionResult{}, oops.Wrapf(err, "CannedClient.Generate lookup")
	}

	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		// Unparseable row — fall through to hardcoded default rather than
		// erroring; the demo MUST keep flowing.
		return hardcodedDefault(templateName, vars,
			"canned row JSON unparseable: "+err.Error()), nil
	}

	return ActionResult{
		Template: templateName,
		Raw:      string(raw),
		Parsed:   parsed,
		Source:   "canned",
	}, nil
}
