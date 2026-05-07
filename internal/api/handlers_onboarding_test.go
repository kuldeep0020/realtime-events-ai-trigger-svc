package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/db"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/sse"
)

// openTestPool opens a real pgxpool using TEST_DATABASE_URL and runs
// migrations. Returns nil and skips the test when the env var is not set.
func openTestPool(t *testing.T) interface {
	// We return *pgxpool.Pool but keep the import lean via the db package.
} {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping DB integration test")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	if err := db.RunMigrationsUp(ctx, pool, "../../migrations"); err != nil {
		db.Close(pool)
		t.Fatalf("RunMigrationsUp: %v", err)
	}
	t.Cleanup(func() { db.Close(pool) })
	return pool
}

// TestActivateConfig_PromotesSeededConfig is the canonical regression test for
// the wizard "Activate & continue" bug.
//
// Setup: two configs for persona "test-persona-activate":
//   - seeded:  has one enabled rule, active=FALSE
//   - stale:   no rules,             active=FALSE (simulates prior wizard run)
//
// After calling POST /api/onboarding/activate with the persona name:
//   - seeded config must have active=TRUE
//   - stale config must have active=FALSE
//   - no new config rows must have been created
func TestActivateConfig_PromotesSeededConfig(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping DB integration test")
	}

	ctx := context.Background()
	pool, err := db.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	if err := db.RunMigrationsUp(ctx, pool, "../../migrations"); err != nil {
		db.Close(pool)
		t.Fatalf("RunMigrationsUp: %v", err)
	}
	t.Cleanup(func() { db.Close(pool) })

	const persona = "test-persona-activate"

	// Clean up any rows left from a previous run of this test.
	_, _ = pool.Exec(ctx, `DELETE FROM configs WHERE persona = $1`, persona)

	// Insert the seeded config (oldest, has a rule).
	var seededID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO configs (tenant_id, persona, config_yaml, active, created_at)
		VALUES ('default', $1, 'persona: test', FALSE, $2)
		RETURNING id`, persona, time.Now().Add(-10*time.Minute)).Scan(&seededID); err != nil {
		t.Fatalf("insert seeded config: %v", err)
	}

	// Attach an enabled rule to the seeded config.
	if _, err := pool.Exec(ctx, `
		INSERT INTO rules (config_id, name, spec, enabled)
		VALUES ($1, 'test_rule', '{"type":"test"}', TRUE)`, seededID); err != nil {
		t.Fatalf("insert rule: %v", err)
	}

	// Insert a stale empty config (newer, no rules — simulates a prior wizard run).
	var staleID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO configs (tenant_id, persona, config_yaml, active, created_at)
		VALUES ('default', $1, '', FALSE, $2)
		RETURNING id`, persona, time.Now()).Scan(&staleID); err != nil {
		t.Fatalf("insert stale config: %v", err)
	}

	// Wire a server with the real pool.
	seed := NewMapSeedFS(map[string][]byte{
		"persona-configs/realestate.yaml": []byte("persona: realestate\n"),
		"persona-configs/rs-self.yaml":    []byte("persona: rs-self\n"),
	})
	srv := New(Config{
		Pool: pool,
		Hub:  sse.NewHub(sse.WithHeartbeatInterval(100 * time.Millisecond)),
		Seed: seed,
	})
	t.Cleanup(func() { _ = srv.hub.Close(context.Background()) })

	// POST /api/onboarding/activate with persona only (wizard style).
	body, _ := json.Marshal(map[string]string{
		"persona":     persona,
		"config_id":   "any",
		"config_yaml": "",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/activate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp activateConfigResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Active {
		t.Error("response.active should be true")
	}
	if resp.ID != seededID {
		t.Errorf("response.id should be seeded config %s, got %s", seededID, resp.ID)
	}

	// Verify DB state.
	var seededActive, staleActive bool
	if err := pool.QueryRow(ctx, `SELECT active FROM configs WHERE id = $1`, seededID).Scan(&seededActive); err != nil {
		t.Fatalf("query seeded active: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT active FROM configs WHERE id = $1`, staleID).Scan(&staleActive); err != nil {
		t.Fatalf("query stale active: %v", err)
	}

	if !seededActive {
		t.Error("seeded config (has rules) should be active=TRUE after activation")
	}
	if staleActive {
		t.Error("stale config (no rules) should be active=FALSE after activation")
	}

	// Verify no new config rows were created — row count should be exactly 2.
	var rowCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM configs WHERE persona = $1`, persona).Scan(&rowCount); err != nil {
		t.Fatalf("count configs: %v", err)
	}
	if rowCount != 2 {
		t.Errorf("expected exactly 2 config rows for persona, got %d (handler must not insert new rows)", rowCount)
	}
}

// TestActivateConfig_PersonaNotFound returns 404 when there are no configs
// with rules for the requested persona.
func TestActivateConfig_PersonaNotFound(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping DB integration test")
	}

	ctx := context.Background()
	pool, err := db.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	if err := db.RunMigrationsUp(ctx, pool, "../../migrations"); err != nil {
		db.Close(pool)
		t.Fatalf("RunMigrationsUp: %v", err)
	}
	t.Cleanup(func() { db.Close(pool) })

	seed := NewMapSeedFS(map[string][]byte{})
	srv := New(Config{
		Pool: pool,
		Hub:  sse.NewHub(sse.WithHeartbeatInterval(100 * time.Millisecond)),
		Seed: seed,
	})
	t.Cleanup(func() { _ = srv.hub.Close(context.Background()) })

	body := []byte(`{"persona":"no-such-persona-xyzzy"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/activate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown persona, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestActivateConfig_BadRequest verifies the handler returns 400 when neither
// id nor persona is provided.
func TestActivateConfig_BadRequest(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping DB integration test")
	}

	ctx := context.Background()
	pool, err := db.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { db.Close(pool) })

	seed := NewMapSeedFS(map[string][]byte{})
	srv := New(Config{
		Pool: pool,
		Hub:  sse.NewHub(sse.WithHeartbeatInterval(100 * time.Millisecond)),
		Seed: seed,
	})
	t.Cleanup(func() { _ = srv.hub.Close(context.Background()) })

	body := []byte(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/activate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty body, got %d body=%s", rec.Code, rec.Body.String())
	}
}
