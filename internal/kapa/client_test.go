package kapa_test

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
	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/db"
	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/kapa"
)

// ─── CannedClient (Postgres-backed) ────────────────────────────────────────

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

func seedCannedKapa(t *testing.T, pool *pgxpool.Pool, pattern string, r kapa.Result) {
	t.Helper()
	body, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal canned kapa: %v", err)
	}
	_, err = pool.Exec(context.Background(),
		`INSERT INTO canned_kapa_responses (query_pattern, response_json) VALUES ($1, $2)`,
		pattern, body,
	)
	if err != nil {
		t.Fatalf("seed canned_kapa: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM canned_kapa_responses WHERE query_pattern = $1`, pattern,
		)
	})
}

func TestCannedClient_ExactMatch(t *testing.T) {
	pool := openTestPool(t)
	pattern := "How do I configure Amplitude destination keys"
	seedCannedKapa(t, pool, pattern, kapa.Result{
		Answer:      "Paste the ingestion key into the destination form.",
		IsUncertain: false,
		RelevantSources: []kapa.Source{
			{Title: "Amplitude destination", SourceURL: "https://example.com/amp"},
		},
	})

	c := kapa.NewCannedClient(pool)
	got, err := c.Retrieve(context.Background(), pattern)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if got.Source != "canned" {
		t.Errorf("expected Source=canned, got %s", got.Source)
	}
	if got.Answer != "Paste the ingestion key into the destination form." {
		t.Errorf("answer mismatch: %q", got.Answer)
	}
	if len(got.RelevantSources) != 1 || got.RelevantSources[0].Title != "Amplitude destination" {
		t.Errorf("sources mismatch: %+v", got.RelevantSources)
	}
}

func TestCannedClient_LikePatternMatch(t *testing.T) {
	pool := openTestPool(t)
	// Pattern uses SQL LIKE wildcards; longest-match should win.
	short := "Amplitude API key %"
	long := "Amplitude API key error %"
	seedCannedKapa(t, pool, short, kapa.Result{Answer: "short answer"})
	seedCannedKapa(t, pool, long, kapa.Result{Answer: "long answer"})

	c := kapa.NewCannedClient(pool)
	got, err := c.Retrieve(context.Background(), "Amplitude API key error during onboarding")
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	// Longest matching pattern wins.
	if got.Answer != "long answer" {
		t.Errorf("expected long-pattern answer, got %q", got.Answer)
	}
	if got.Source != "canned" {
		t.Errorf("expected Source=canned, got %s", got.Source)
	}
}

func TestCannedClient_MissReturnsFallback(t *testing.T) {
	pool := openTestPool(t)
	c := kapa.NewCannedClient(pool)

	got, err := c.Retrieve(context.Background(), "totally unknown query that matches nothing")
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if got.Source != "fallback" {
		t.Errorf("expected Source=fallback on miss, got %s", got.Source)
	}
	if !got.IsUncertain {
		t.Error("expected IsUncertain=true on fallback")
	}
	if got.Answer == "" {
		t.Error("fallback Answer must not be empty")
	}
	if got.RelevantSources == nil {
		t.Error("RelevantSources should be empty slice, not nil")
	}
}

func TestCannedClient_NilPoolFallback(t *testing.T) {
	c := kapa.NewCannedClient(nil)
	got, err := c.Retrieve(context.Background(), "x")
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if got.Source != "fallback" {
		t.Errorf("expected fallback for nil pool, got %s", got.Source)
	}
}

// ─── LiveClient (httptest, no real network) ────────────────────────────────

func TestLiveClient_RequestShape(t *testing.T) {
	var captured struct {
		method  string
		path    string
		apiKey  string
		ctype   string
		body    map[string]string
	}

	canonical := kapa.Result{
		Answer:      "Real Kapa response.",
		IsUncertain: false,
		RelevantSources: []kapa.Source{
			{Title: "Doc 1", SourceURL: "https://rudderstack.com/docs/x"},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.method = r.Method
		captured.path = r.URL.Path
		captured.apiKey = r.Header.Get("X-API-KEY")
		captured.ctype = r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &captured.body)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(canonical)
	}))
	defer srv.Close()

	c, err := kapa.NewLiveClient(kapa.LiveConfig{
		BaseURL:   srv.URL,
		ProjectID: "proj-abc",
		APIKey:    "sk-live-test",
		Timeout:   2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewLiveClient: %v", err)
	}

	got, err := c.Retrieve(context.Background(), "How do I fix my onboarding error?")
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}

	if captured.method != http.MethodPost {
		t.Errorf("expected POST, got %s", captured.method)
	}
	wantPath := "/query/v1/projects/proj-abc/chat/"
	if captured.path != wantPath {
		t.Errorf("expected path %s, got %s", wantPath, captured.path)
	}
	if captured.apiKey != "sk-live-test" {
		t.Errorf("expected X-API-KEY header, got %q", captured.apiKey)
	}
	if !strings.HasPrefix(captured.ctype, "application/json") {
		t.Errorf("expected JSON content type, got %q", captured.ctype)
	}
	if captured.body["query"] != "How do I fix my onboarding error?" {
		t.Errorf("body query mismatch: %+v", captured.body)
	}
	if got.Source != "live" {
		t.Errorf("expected Source=live, got %s", got.Source)
	}
	if got.Answer != "Real Kapa response." {
		t.Errorf("answer mismatch: %q", got.Answer)
	}
}

func TestLiveClient_RequiresProjectAndKey(t *testing.T) {
	if _, err := kapa.NewLiveClient(kapa.LiveConfig{APIKey: "x"}); err == nil {
		t.Error("expected error for missing ProjectID")
	}
	if _, err := kapa.NewLiveClient(kapa.LiveConfig{ProjectID: "x"}); err == nil {
		t.Error("expected error for missing APIKey")
	}
}

func TestLiveClient_NonOKErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid api key"}`))
	}))
	defer srv.Close()

	c, err := kapa.NewLiveClient(kapa.LiveConfig{
		BaseURL:   srv.URL,
		ProjectID: "p",
		APIKey:    "k",
	})
	if err != nil {
		t.Fatalf("NewLiveClient: %v", err)
	}
	_, err = c.Retrieve(context.Background(), "q")
	if err == nil {
		t.Fatal("expected error for 401")
	}
}
