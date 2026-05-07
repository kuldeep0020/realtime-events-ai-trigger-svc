package activation_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/activation"
	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/db"
)

// ─── MockClient (Postgres-backed) ──────────────────────────────────────────

func openTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping integration test")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	if err := db.RunMigrationsUp(ctx, pool, "../../migrations"); err != nil {
		t.Fatalf("RunMigrationsUp: %v", err)
	}
	t.Cleanup(func() { db.Close(pool) })
	return pool
}

func seedProfile(t *testing.T, pool *pgxpool.Pool, entity, idType, idValue string, traits map[string]any) {
	t.Helper()
	body, err := json.Marshal(traits)
	if err != nil {
		t.Fatalf("marshal traits: %v", err)
	}
	_, err = pool.Exec(context.Background(), `
		INSERT INTO mock_profiles (entity, id_type, id_value, traits)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (entity, id_type, id_value)
		DO UPDATE SET traits = EXCLUDED.traits, updated_at = NOW()`,
		entity, idType, idValue, body,
	)
	if err != nil {
		t.Fatalf("seed mock_profile: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM mock_profiles WHERE entity=$1 AND id_type=$2 AND id_value=$3`,
			entity, idType, idValue,
		)
	})
}

func TestMockClient_Hit(t *testing.T) {
	pool := openTestPool(t)
	const idValue = "anon_test_hit"
	seedProfile(t, pool, "user", "anonymous_id", idValue, map[string]any{
		"plan":           "pro",
		"total_sessions": float64(12),
		"favorites":      []any{"x", "y"},
	})

	c := activation.NewMockClient(pool)
	resp, err := c.GetProfile(context.Background(), activation.ProfileRequest{
		Entity:        "user",
		DestinationID: "mock-redis-1",
		ID:            activation.ID{Type: "anonymous_id", Value: idValue},
	})
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if resp.Entity != "user" || resp.ID.Type != "anonymous_id" || resp.ID.Value != idValue {
		t.Errorf("unexpected envelope: %+v", resp)
	}
	if resp.Data["plan"] != "pro" {
		t.Errorf("expected plan=pro, got %v", resp.Data["plan"])
	}
}

func TestMockClient_Miss_ReturnsEmptyDataNotNil(t *testing.T) {
	pool := openTestPool(t)

	c := activation.NewMockClient(pool)
	resp, err := c.GetProfile(context.Background(), activation.ProfileRequest{
		Entity:        "user",
		DestinationID: "mock-redis-1",
		ID:            activation.ID{Type: "anonymous_id", Value: "nope-no-such-id"},
	})
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if resp.Data == nil {
		t.Fatal("Data must be non-nil empty map on miss, got nil")
	}
	if len(resp.Data) != 0 {
		t.Errorf("expected empty data, got %v", resp.Data)
	}
}

func TestMockClient_IdentityResolutionFallback_UserIDtoAnonymousID(t *testing.T) {
	pool := openTestPool(t)
	const sharedID = "demo-rs-fallback-001"

	// Seed only under anonymous_id; lookup under user_id should fall back.
	seedProfile(t, pool, "user", "anonymous_id", sharedID, map[string]any{"plan": "free"})

	c := activation.NewMockClient(pool)
	resp, err := c.GetProfile(context.Background(), activation.ProfileRequest{
		Entity:        "user",
		DestinationID: "mock-redis-1",
		ID:            activation.ID{Type: "user_id", Value: sharedID},
	})
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if resp.Data["plan"] != "free" {
		t.Errorf("expected fallback to find plan=free; got %v", resp.Data)
	}
	// Envelope echoes the ORIGINAL request id, not the fallback id.
	if resp.ID.Type != "user_id" {
		t.Errorf("expected echoed request id type=user_id, got %s", resp.ID.Type)
	}
}

func TestMockClient_IdentityResolutionFallback_NoLoopForAnonymousID(t *testing.T) {
	pool := openTestPool(t)
	c := activation.NewMockClient(pool)

	// anonymous_id miss must NOT fall back (no infinite chain).
	resp, err := c.GetProfile(context.Background(), activation.ProfileRequest{
		Entity:        "user",
		DestinationID: "mock-redis-1",
		ID:            activation.ID{Type: "anonymous_id", Value: "completely-unseeded"},
	})
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if len(resp.Data) != 0 {
		t.Errorf("expected empty data for anonymous_id miss, got %v", resp.Data)
	}
}

func TestMockClient_RejectsEmptyRequest(t *testing.T) {
	c := activation.NewMockClient(nil) // pool nil triggers nil-guard before any DB call
	_, err := c.GetProfile(context.Background(), activation.ProfileRequest{})
	if err == nil {
		t.Fatal("expected error for empty request / nil pool")
	}
}

// ─── LiveClient (httptest, no real network) ────────────────────────────────

func TestLiveClient_RequestShape(t *testing.T) {
	var captured struct {
		method string
		path   string
		auth   string
		ctype  string
		body   activation.ProfileRequest
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.method = r.Method
		captured.path = r.URL.Path
		captured.auth = r.Header.Get("Authorization")
		captured.ctype = r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &captured.body)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(activation.ProfileResponse{
			Entity: captured.body.Entity,
			ID:     captured.body.ID,
			Data: map[string]any{
				"plan": "enterprise",
			},
		})
	}))
	defer srv.Close()

	c, err := activation.NewLiveClient(activation.LiveConfig{
		BaseURL: srv.URL,
		SAT:     "test-sat-token",
		Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewLiveClient: %v", err)
	}

	req := activation.ProfileRequest{
		Entity:        "user",
		DestinationID: "mock-redis-1",
		ID:            activation.ID{Type: "anonymous_id", Value: "anon_demo-re-001"},
	}
	resp, err := c.GetProfile(context.Background(), req)
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}

	if captured.method != http.MethodPost {
		t.Errorf("expected POST, got %s", captured.method)
	}
	if captured.path != "/activation" {
		t.Errorf("expected /activation, got %s", captured.path)
	}
	if captured.auth != "Bearer test-sat-token" {
		t.Errorf("expected Bearer auth, got %q", captured.auth)
	}
	if !strings.HasPrefix(captured.ctype, "application/json") {
		t.Errorf("expected JSON content type, got %q", captured.ctype)
	}
	if captured.body.Entity != "user" || captured.body.DestinationID != "mock-redis-1" ||
		captured.body.ID.Type != "anonymous_id" || captured.body.ID.Value != "anon_demo-re-001" {
		t.Errorf("body did not match request: %+v", captured.body)
	}
	if resp.Data["plan"] != "enterprise" {
		t.Errorf("response decode mismatch: %+v", resp)
	}
}

func TestLiveClient_RequiresBaseURLAndSAT(t *testing.T) {
	if _, err := activation.NewLiveClient(activation.LiveConfig{SAT: "x"}); err == nil {
		t.Error("expected error for missing BaseURL")
	}
	if _, err := activation.NewLiveClient(activation.LiveConfig{BaseURL: "https://x"}); err == nil {
		t.Error("expected error for missing SAT")
	}
}

func TestLiveClient_NonJSONResponseTreatedAsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer srv.Close()

	c, err := activation.NewLiveClient(activation.LiveConfig{
		BaseURL: srv.URL,
		SAT:     "x",
	})
	if err != nil {
		t.Fatalf("NewLiveClient: %v", err)
	}
	_, err = c.GetProfile(context.Background(), activation.ProfileRequest{
		Entity:        "user",
		DestinationID: "d",
		ID:            activation.ID{Type: "anonymous_id", Value: "v"},
	})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}
