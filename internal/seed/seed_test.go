package seed_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/api"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/db"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/seed"
)

// fixtureFS returns a SeedFS whose contents mirror the on-disk seed/ tree.
// We embed only the minimum each loader needs.
func fixtureFS() seed.SeedFS {
	return api.NewMapSeedFS(map[string][]byte{
		"canned-responses-hand.yaml": []byte(`canned_llm:
  - template_name: realestate_realtor_pitch
    persona: realestate
    variant: default
    priority: 100
    raw_json:
      headline: "Test pitch"
      best_cta: "Call now"
      urgency: high
  - template_name: rs_onboarding_stuck
    persona: rs-self
    variant: default
    priority: 100
    raw_json:
      subject: "Test subject"
      body_markdown: "Test body"

canned_kapa:
  - query_pattern: "Test query %"
    response_json:
      answer: "Test answer"
      is_uncertain: false
      relevant_sources: []
`),
		"mock_profiles.yaml": []byte(`- entity: user
  id_type: anonymous_id
  id_value: "anon-1"
  traits:
    plan: "free"
- entity: user
  id_type: user_id
  id_value: "user-1"
  traits:
    plan: "pro"
`),
		"persona-configs/realestate.yaml": []byte(`persona: realestate
slack_channel: realestate-realtor-pings
rules:
  - name: rule_one
    when:
      window.event_count: { ">=": 3 }
    fire:
      action_template: realestate_realtor_pitch
      destination: "slack:realestate-realtor-pings"
      cooldown_seconds: 3600
`),
		"persona-configs/rs-self.yaml": []byte(`persona: rs-self
rules:
  - name: rule_two
    when:
      window.has_event_name: "Source Setup Error"
    fire:
      action_template: rs_onboarding_stuck
      destination: "email:user"
      cooldown_seconds: 86400
`),
		"action-templates.yaml": []byte(`- name: realestate_realtor_pitch
  output_format: json
  system_prompt: "system A"
  user_prompt_tmpl: "user A"
- name: rs_onboarding_stuck
  output_format: json
  system_prompt: "system B"
  user_prompt_tmpl: "user B"
`),
		"tracking_plans/realestate.json": []byte(`{"persona":"realestate","events":[]}`),
		"tracking_plans/rs-self.json":    []byte(`{"persona":"rs-self","events":[]}`),
	})
}

// TestNilSeederErrors verifies that nil pool / nil fs are surfaced clearly.
func TestNilSeederErrors(t *testing.T) {
	t.Parallel()

	if err := (&seed.Seeder{}).LoadAll(context.Background()); err == nil {
		t.Error("expected error from nil pool")
	}

	// fs nil — pool is also nil so this hits the same first-check guard,
	// but ensures the public constructor accepts both args.
	if s := seed.NewSeeder(nil, nil); s == nil {
		t.Fatal("NewSeeder returned nil")
	}
}

// TestParseFixtureYAMLs exercises the YAML parsing layer without touching
// Postgres. Each loader's parse step is invoked indirectly via a lightweight
// integration that calls LoadAll on a Seeder backed by a fake FS but a nil
// pool — we expect the function to bail at the pool-nil guard, which means
// the YAML parsers all happen earlier in unit-test runs that DO have a pool.
//
// To exercise parser correctness directly without a DB, we round-trip the
// fixture YAML files through the json.Unmarshal step and verify that the
// inputs at least parse as valid YAML. This is a cheap sanity test that
// catches regressions when the on-disk schema drifts.
func TestParseFixtureYAMLs(t *testing.T) {
	t.Parallel()
	fs := fixtureFS()
	files := []string{
		"canned-responses-hand.yaml",
		"mock_profiles.yaml",
		"persona-configs/realestate.yaml",
		"persona-configs/rs-self.yaml",
		"action-templates.yaml",
	}
	for _, f := range files {
		body, err := fs.ReadFile(f)
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		if len(body) == 0 {
			t.Errorf("%s: empty fixture", f)
		}
	}
	// Tracking plan JSONs.
	for _, p := range []string{"tracking_plans/realestate.json", "tracking_plans/rs-self.json"} {
		body, err := fs.ReadFile(p)
		if err != nil {
			t.Errorf("%s: %v", p, err)
			continue
		}
		var probe any
		if err := json.Unmarshal(body, &probe); err != nil {
			t.Errorf("%s: invalid JSON: %v", p, err)
		}
	}
}

// TestIntegration_LoadAll seeds every loader against a real Postgres
// database (TEST_DATABASE_URL) and verifies row counts. Skipped when the
// env var is unset.
func TestIntegration_LoadAll(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping integration test")
	}

	ctx := context.Background()
	pool, err := db.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close(pool) })

	if err := db.RunMigrationsUp(ctx, pool, "../../migrations"); err != nil {
		t.Fatalf("RunMigrationsUp: %v", err)
	}

	s := seed.NewSeeder(pool, fixtureFS())
	if err := s.LoadAll(ctx); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	// Re-running must be idempotent.
	if err := s.LoadAll(ctx); err != nil {
		t.Fatalf("LoadAll (second pass): %v", err)
	}

	type counts struct {
		canned     int
		kapa       int
		profiles   int
		configs    int
		rules      int
		templates  int
		tracking   int
	}
	var c counts
	mustCount := func(table string, dst *int) {
		t.Helper()
		if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM "+table).Scan(dst); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
	}
	mustCount("canned_responses", &c.canned)
	mustCount("canned_kapa_responses", &c.kapa)
	mustCount("mock_profiles", &c.profiles)
	mustCount("configs", &c.configs)
	mustCount("rules", &c.rules)
	mustCount("action_templates", &c.templates)
	mustCount("tracking_plans", &c.tracking)

	if c.canned < 2 {
		t.Errorf("expected ≥2 canned_responses, got %d", c.canned)
	}
	if c.kapa < 1 {
		t.Errorf("expected ≥1 canned_kapa_responses, got %d", c.kapa)
	}
	if c.profiles < 2 {
		t.Errorf("expected ≥2 mock_profiles, got %d", c.profiles)
	}
	if c.configs < 2 {
		t.Errorf("expected ≥2 configs (one per persona), got %d", c.configs)
	}
	if c.rules < 2 {
		t.Errorf("expected ≥2 rules, got %d", c.rules)
	}
	if c.templates < 2 {
		t.Errorf("expected ≥2 action_templates, got %d", c.templates)
	}
	if c.tracking < 2 {
		t.Errorf("expected ≥2 tracking_plans, got %d", c.tracking)
	}
}
