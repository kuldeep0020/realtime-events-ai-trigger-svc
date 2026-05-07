package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/db"
	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/sse"
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

// --- Generate-config answer application tests (no DB required) ---

// realestateFullSeed is the realestate persona seed YAML used for answer tests.
// It mirrors seed/persona-configs/realestate.yaml.
const realestateFullSeed = `persona: realestate
realtors:
- name: Priya N.
  suburbs:
  - suburb-1
  - suburb-2
  hours: 09:00-18:00 IST
- name: Arjun M.
  suburbs:
  - suburb-3
  hours: 10:00-19:00 IST
slack_channel: realestate-realtor-pings
rules:
- name: realtor_session_abandoned
  when:
    all:
    - window.event_count:
        '>=': 3
    - window.event_path_matches: ^/listings(/|$).*
    - window.idle_seconds:
        '>=': 10
    - window.has_event_type: page
  fire:
    action_template: realestate_realtor_pitch
    destination: slack:realestate-realtor-pings
    cooldown_seconds: 3600
`

// rsSelfFullSeed mirrors seed/persona-configs/rs-self.yaml.
const rsSelfFullSeed = `persona: rs-self
rules:
- name: onboarding_errored
  when:
    any:
    - window.has_event_name: Source Setup Error
    - window.has_event_name: Destination Setup Error
    - window.has_event_name: Webhook Send Error
  fire:
    action_template: rs_destination_error
    destination: email:user
    cooldown_seconds: 86400
- name: onboarding_stuck
  when:
    all:
    - window.has_event_name: Source Created
    - window.event_count_of_name:
        name: Destination Created
        '==': 0
    - window.idle_seconds:
        '>=': 15
  fire:
    action_template: rs_onboarding_stuck
    destination: email:user
    cooldown_seconds: 86400
`

// newFullSeedServer returns a Server wired with realistic seed files.
func newFullSeedServer(t *testing.T) *Server {
	t.Helper()
	seed := NewMapSeedFS(map[string][]byte{
		"persona-configs/realestate.yaml": []byte(realestateFullSeed),
		"persona-configs/rs-self.yaml":    []byte(rsSelfFullSeed),
	})
	srv := New(Config{
		Pool: nil,
		Hub:  sse.NewHub(sse.WithHeartbeatInterval(20 * time.Millisecond)),
		Seed: seed,
	})
	t.Cleanup(func() { _ = srv.hub.Close(context.Background()) })
	return srv
}

// postGenerateConfig is a test helper that calls POST /api/onboarding/generate-config
// with the given body and returns the parsed response.
func postGenerateConfig(t *testing.T, srv *Server, body any) (int, generateConfigResponse) {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/generate-config", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	var resp generateConfigResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	return rec.Code, resp
}

// TestGenerateConfig_RealestateAppliesIdleSeconds verifies that supplying
// idle_seconds in answers replaces the seed's idle threshold in the generated YAML.
func TestGenerateConfig_RealestateAppliesIdleSeconds(t *testing.T) {
	t.Parallel()
	srv := newFullSeedServer(t)

	code, resp := postGenerateConfig(t, srv, map[string]any{
		"persona": "realestate",
		"answers": map[string]any{"idle_seconds": 5},
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}

	yaml := resp.ConfigYAML
	// The generated YAML must contain the user's idle_seconds predicate value.
	// The seed has ">= 10"; the answer overrides it to ">= 5".
	// We look for the specific pattern "'>=': 5" in the YAML output.
	if !strings.Contains(yaml, "'>=': 5") {
		t.Errorf("expected \"'>=':\\: 5\" in generated YAML; got:\n%s", yaml)
	}
	// The old seed idle threshold "'>=': 10" must not remain.
	if strings.Contains(yaml, "'>=': 10") {
		t.Errorf("old idle threshold \"'>=':\\: 10\" still present in generated YAML:\n%s", yaml)
	}
}

// TestGenerateConfig_RealestateAppliesRealtors verifies that supplying a
// realtors textarea answer rebuilds the realtors block correctly.
func TestGenerateConfig_RealestateAppliesRealtors(t *testing.T) {
	t.Parallel()
	srv := newFullSeedServer(t)

	code, resp := postGenerateConfig(t, srv, map[string]any{
		"persona": "realestate",
		"answers": map[string]any{
			"realtors": "Test One → s1, s2\nTest Two → s3",
		},
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}

	yaml := resp.ConfigYAML
	if !strings.Contains(yaml, "Test One") {
		t.Errorf("expected 'Test One' in realtors block; got:\n%s", yaml)
	}
	if !strings.Contains(yaml, "Test Two") {
		t.Errorf("expected 'Test Two' in realtors block; got:\n%s", yaml)
	}
	if !strings.Contains(yaml, "s1") || !strings.Contains(yaml, "s2") {
		t.Errorf("expected suburbs s1 and s2 in realtors block; got:\n%s", yaml)
	}
	if !strings.Contains(yaml, "s3") {
		t.Errorf("expected suburb s3 in realtors block; got:\n%s", yaml)
	}
	// Original realtors Priya/Arjun must not appear.
	if strings.Contains(yaml, "Priya") || strings.Contains(yaml, "Arjun") {
		t.Errorf("original realtors should have been replaced; got:\n%s", yaml)
	}
}

// TestGenerateConfig_RSSelfNarrowsErrorEvents verifies that supplying a single
// error_events answer rebuilds the onboarding_errored rule's `any` block with
// only that event name.
func TestGenerateConfig_RSSelfNarrowsErrorEvents(t *testing.T) {
	t.Parallel()
	srv := newFullSeedServer(t)

	code, resp := postGenerateConfig(t, srv, map[string]any{
		"persona": "rs-self",
		"answers": map[string]any{
			"error_events": []any{"Destination Setup Error"},
		},
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}

	yaml := resp.ConfigYAML
	// The narrowed event name must appear.
	if !strings.Contains(yaml, "Destination Setup Error") {
		t.Errorf("expected 'Destination Setup Error' in generated YAML; got:\n%s", yaml)
	}
	// The other two events must NOT appear (they were de-selected).
	if strings.Contains(yaml, "Source Setup Error") {
		t.Errorf("'Source Setup Error' should have been removed; got:\n%s", yaml)
	}
	if strings.Contains(yaml, "Webhook Send Error") {
		t.Errorf("'Webhook Send Error' should have been removed; got:\n%s", yaml)
	}
}

// TestGenerateConfig_NoAnswersReturnsSeedVerbatim verifies that omitting
// answers returns the seed YAML unchanged (legacy client / test compatibility).
func TestGenerateConfig_NoAnswersReturnsSeedVerbatim(t *testing.T) {
	t.Parallel()
	srv := newFullSeedServer(t)

	code, resp := postGenerateConfig(t, srv, map[string]any{
		"persona": "realestate",
		// No "answers" key → should return seed verbatim.
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}

	// The returned YAML must equal the seed exactly.
	if strings.TrimSpace(resp.ConfigYAML) != strings.TrimSpace(realestateFullSeed) {
		t.Errorf("expected seed YAML verbatim, got:\n%s\nwant:\n%s", resp.ConfigYAML, realestateFullSeed)
	}
}

// --- Activate input-validation tests (no DB required) ---

// TestActivateConfig_BadYAML_400 verifies that a syntactically invalid
// config_yaml value returns HTTP 400 (user error, not server error) with a
// message that starts with "invalid config_yaml".
func TestActivateConfig_BadYAML_400(t *testing.T) {
	t.Parallel()
	srv := New(Config{
		Pool: nil, // no DB — parse error is caught before any DB call
		Hub:  sse.NewHub(sse.WithHeartbeatInterval(20 * time.Millisecond)),
	})
	t.Cleanup(func() { _ = srv.hub.Close(context.Background()) })

	body := []byte(`{"persona":"realestate","config_yaml":"not: valid: yaml: [["}`)
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/activate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad YAML, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid config_yaml") {
		t.Errorf("expected body to contain 'invalid config_yaml', got: %s", rec.Body.String())
	}
}

// TestActivateConfig_YAMLWithoutRules_400 verifies that a well-formed YAML
// that contains no rules block returns HTTP 400 with a clear message.
func TestActivateConfig_YAMLWithoutRules_400(t *testing.T) {
	t.Parallel()
	srv := New(Config{
		Pool: nil, // no DB — empty-rules check is caught before any DB call
		Hub:  sse.NewHub(sse.WithHeartbeatInterval(20 * time.Millisecond)),
	})
	t.Cleanup(func() { _ = srv.hub.Close(context.Background()) })

	body := []byte(`{"persona":"realestate","config_yaml":"persona: realestate\n"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/activate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for YAML with no rules, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "at least one rule") {
		t.Errorf("expected body to contain 'at least one rule', got: %s", rec.Body.String())
	}
}

// --- Integration tests for activate-with-yaml (require TEST_DATABASE_URL) ---

// newTestServerWithFullSeedAndPool builds a Server with realistic seed YAML
// and a real Postgres pool.
func newTestServerWithFullSeedAndPool(t *testing.T, pool *pgxpool.Pool) *Server {
	t.Helper()
	seed := NewMapSeedFS(map[string][]byte{
		"persona-configs/realestate.yaml": []byte(realestateFullSeed),
		"persona-configs/rs-self.yaml":    []byte(rsSelfFullSeed),
	})
	srv := New(Config{
		Pool: pool,
		Hub:  sse.NewHub(sse.WithHeartbeatInterval(100 * time.Millisecond)),
		Seed: seed,
	})
	t.Cleanup(func() { _ = srv.hub.Close(context.Background()) })
	return srv
}

// TestActivateConfig_PersonaWithConfigYAML_UpdatesRules verifies the full
// wizard customization path: a seeded config with N rules, activated with
// custom YAML containing M rules (with a different idle_seconds), should
// result in M rules persisted to DB with the new idle_seconds value.
func TestActivateConfig_PersonaWithConfigYAML_UpdatesRules(t *testing.T) {
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

	const persona = "test-persona-yaml-replace"

	// Clean up any leftover state from a prior run.
	_, _ = pool.Exec(ctx, `DELETE FROM configs WHERE persona = $1`, persona)

	// Insert a seeded config with 2 rules.
	var seededID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO configs (tenant_id, persona, config_yaml, active, created_at)
		VALUES ('default', $1, 'persona: test', FALSE, $2)
		RETURNING id`, persona, time.Now().Add(-5*time.Minute)).Scan(&seededID); err != nil {
		t.Fatalf("insert seeded config: %v", err)
	}
	for _, ruleName := range []string{"rule_a", "rule_b"} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO rules (config_id, name, spec, enabled)
			VALUES ($1, $2, '{"name":"placeholder","when":{},"fire":{}}', TRUE)`, seededID, ruleName); err != nil {
			t.Fatalf("insert rule %s: %v", ruleName, err)
		}
	}

	// Build custom YAML with idle_seconds=7 (one rule, different from seed's 2).
	customYAML := `persona: ` + persona + `
rules:
- name: custom_rule_idle_7
  when:
    all:
    - window.idle_seconds:
        '>=': 7
  fire:
    action_template: test_template
    destination: email:user
    cooldown_seconds: 3600
`

	srv := newTestServerWithFullSeedAndPool(t, pool)
	body, _ := json.Marshal(map[string]any{
		"persona":     persona,
		"config_yaml": customYAML,
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
		t.Error("expected active=true in response")
	}
	if resp.RulesReplaced != 1 {
		t.Errorf("expected rules_replaced=1, got %d", resp.RulesReplaced)
	}

	// Verify DB: exactly 1 rule now with name "custom_rule_idle_7".
	rows, err := pool.Query(ctx, `SELECT name, spec FROM rules WHERE config_id = $1`, seededID)
	if err != nil {
		t.Fatalf("query rules: %v", err)
	}
	defer rows.Close()

	type ruleRow struct {
		Name string
		Spec []byte
	}
	var dbRules []ruleRow
	for rows.Next() {
		var r ruleRow
		if err := rows.Scan(&r.Name, &r.Spec); err != nil {
			t.Fatalf("scan: %v", err)
		}
		dbRules = append(dbRules, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	if len(dbRules) != 1 {
		t.Fatalf("expected 1 rule in DB, got %d", len(dbRules))
	}
	if dbRules[0].Name != "custom_rule_idle_7" {
		t.Errorf("expected rule name 'custom_rule_idle_7', got %q", dbRules[0].Name)
	}

	// Verify the spec JSONB contains idle_seconds: 7.
	var spec map[string]any
	if err := json.Unmarshal(dbRules[0].Spec, &spec); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}
	specBytes, _ := json.Marshal(spec)
	specStr := string(specBytes)
	if !strings.Contains(specStr, "7") {
		t.Errorf("expected idle_seconds 7 in persisted spec, got: %s", specStr)
	}
}

// TestActivateConfig_PersonaWithoutConfigYAML_FallsBackToSeed verifies that
// when config_yaml is omitted the original seeded config is activated as-is
// (backward compatibility preserved).
func TestActivateConfig_PersonaWithoutConfigYAML_FallsBackToSeed(t *testing.T) {
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

	const persona = "test-persona-no-yaml"
	_, _ = pool.Exec(ctx, `DELETE FROM configs WHERE persona = $1`, persona)

	var seededID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO configs (tenant_id, persona, config_yaml, active, created_at)
		VALUES ('default', $1, 'persona: test-persona-no-yaml', FALSE, $2)
		RETURNING id`, persona, time.Now().Add(-5*time.Minute)).Scan(&seededID); err != nil {
		t.Fatalf("insert seeded config: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO rules (config_id, name, spec, enabled)
		VALUES ($1, 'seed_rule', '{"name":"seed_rule","when":{},"fire":{}}', TRUE)`, seededID); err != nil {
		t.Fatalf("insert rule: %v", err)
	}

	srv := newTestServerWithFullSeedAndPool(t, pool)
	// POST without config_yaml.
	body, _ := json.Marshal(map[string]any{"persona": persona})
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
	if resp.ID != seededID {
		t.Errorf("expected id=%s, got %s", seededID, resp.ID)
	}
	if !resp.Active {
		t.Error("expected active=true")
	}

	// The original seed_rule must still be in the DB (no replacement occurred).
	var ruleCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM rules WHERE config_id = $1 AND name = 'seed_rule'`, seededID).Scan(&ruleCount); err != nil {
		t.Fatalf("count rules: %v", err)
	}
	if ruleCount != 1 {
		t.Errorf("expected seed_rule still in DB, got count=%d", ruleCount)
	}
}
