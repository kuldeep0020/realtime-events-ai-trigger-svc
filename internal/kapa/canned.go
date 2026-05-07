package kapa

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/db"
	"github.com/samber/oops"
)

// CannedClient implements Client by reading from Postgres
// `canned_kapa_responses`.
//
// Lookup ordering (delegated to db.LoadCannedKapa):
//
//  1. Exact match on `query_pattern = $1`
//  2. SQL LIKE match where the stored pattern contains wildcards
//     (`$1 LIKE query_pattern`), with longest pattern wins via
//     `ORDER BY length(query_pattern) DESC LIMIT 1`.
//
// On any miss (no rows OR pgx error of type ErrNoRows) the client returns
// `defaultCannedResult` rather than an error — the demo path must always
// produce something renderable.
type CannedClient struct {
	pool *pgxpool.Pool
}

// NewCannedClient builds a CannedClient backed by an existing pool.
func NewCannedClient(pool *pgxpool.Pool) *CannedClient {
	return &CannedClient{pool: pool}
}

// Retrieve looks up a canned response by query pattern, falling back to a
// generic uncertain answer when nothing matches. A nil pool is treated as a
// permanent miss (same fallback) to keep wiring tests trivial.
func (c *CannedClient) Retrieve(ctx context.Context, query string) (Result, error) {
	if c == nil || c.pool == nil {
		return defaultCannedResult(query), nil
	}

	raw, err := db.LoadCannedKapa(ctx, c.pool, query)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return defaultCannedResult(query), nil
		}
		return Result{}, oops.Wrapf(err, "CannedClient.Retrieve lookup")
	}

	var r Result
	if err := json.Unmarshal(raw, &r); err != nil {
		return Result{}, oops.Wrapf(err, "CannedClient.Retrieve unmarshal")
	}
	if r.RelevantSources == nil {
		r.RelevantSources = []Source{}
	}
	r.Source = "canned"
	return r, nil
}
