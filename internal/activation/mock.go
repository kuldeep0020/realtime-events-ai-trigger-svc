package activation

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/db"
	"github.com/samber/oops"
)

// MockClient implements Client by reading from Postgres `mock_profiles`.
//
// Identity-resolution fallback (§3.5): the real Activation API lets a single
// profile be addressable by multiple identifier types. The hackathon mocks
// this by attempting a second lookup with `anonymous_id` when a `user_id`
// lookup misses, using the same value. This mirrors the rs-self persona
// where the demo seeds the same id under both id_types.
type MockClient struct {
	pool *pgxpool.Pool
}

// NewMockClient builds a MockClient backed by an existing pool. The pool is
// not owned by the client — callers manage its lifecycle.
func NewMockClient(pool *pgxpool.Pool) *MockClient {
	return &MockClient{pool: pool}
}

// GetProfile looks up traits in `mock_profiles` for the request's
// (entity, id.type, id.value). On miss for `user_id`, it transparently
// retries with `anonymous_id` of the same value. Always returns a non-nil
// `Data` map — empty when no profile exists, matching the live API.
func (c *MockClient) GetProfile(ctx context.Context, req ProfileRequest) (ProfileResponse, error) {
	if c == nil || c.pool == nil {
		return ProfileResponse{}, oops.Errorf("MockClient: pool is nil")
	}
	if req.Entity == "" || req.ID.Type == "" || req.ID.Value == "" {
		return ProfileResponse{}, oops.Errorf("MockClient: entity, id.type, id.value required")
	}

	traits, err := db.LoadProfile(ctx, c.pool, req.Entity, req.ID.Type, req.ID.Value)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ProfileResponse{}, oops.Wrapf(err, "MockClient.GetProfile lookup")
	}

	// Identity-resolution fallback: user_id miss → try anonymous_id of same value.
	if errors.Is(err, pgx.ErrNoRows) && req.ID.Type == "user_id" {
		traits, err = db.LoadProfile(ctx, c.pool, req.Entity, "anonymous_id", req.ID.Value)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return ProfileResponse{}, oops.Wrapf(err, "MockClient.GetProfile fallback")
		}
	}

	resp := ProfileResponse{
		Entity: req.Entity,
		ID:     req.ID,
		Data:   map[string]any{},
	}
	if errors.Is(err, pgx.ErrNoRows) || len(traits) == 0 {
		return resp, nil
	}

	if err := json.Unmarshal(traits, &resp.Data); err != nil {
		return ProfileResponse{}, oops.Wrapf(err, "MockClient.GetProfile unmarshal traits")
	}
	if resp.Data == nil {
		// JSON null in the column — surface as empty map per the v1 contract.
		resp.Data = map[string]any{}
	}
	return resp, nil
}
