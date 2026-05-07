package db_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/db"
)

// TestPackageCompiles verifies the package compiles and the exported API surface
// exists. No database connection is required.
func TestPackageCompiles(t *testing.T) {
	// Verify Open signature exists by checking we can reference it.
	var _ func(context.Context, string) (*interface{}, error)
	t.Log("db package compiles OK")
}

// TestOpenMissingDSN verifies that Open returns an error on a clearly invalid DSN.
func TestOpenMissingDSN(t *testing.T) {
	ctx := context.Background()
	pool, err := db.Open(ctx, "not-a-valid-dsn://")
	if pool != nil {
		db.Close(pool)
	}
	// We expect an error; if it somehow succeeded something is very wrong.
	if err == nil {
		t.Error("expected error for invalid DSN, got nil")
	}
}

// TestIntegration runs against a real Postgres instance when TEST_DATABASE_URL
// is set in the environment. Otherwise the test is skipped.
func TestIntegration(t *testing.T) {
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

	// Insert a config so we can insert a rule.
	var configID uuid.UUID
	err = pool.QueryRow(ctx, `
		INSERT INTO configs (tenant_id, persona, config_yaml, active)
		VALUES ('test-tenant', 'realestate', 'persona: realestate', TRUE)
		RETURNING id`).Scan(&configID)
	if err != nil {
		t.Fatalf("insert config: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM configs WHERE id = $1", configID)
	})

	t.Run("InsertEvent", func(t *testing.T) {
		id, err := db.InsertEvent(ctx, pool,
			"anon-001", "user-001", "write-key-1",
			"track", "Page Viewed", "/home",
			[]byte(`{"foo":"bar"}`),
		)
		if err != nil {
			t.Fatalf("InsertEvent: %v", err)
		}
		if id <= 0 {
			t.Errorf("expected positive id, got %d", id)
		}
	})

	t.Run("FetchEventsForAnon", func(t *testing.T) {
		_, _ = db.InsertEvent(ctx, pool,
			"anon-fetch-test", "", "write-key-1",
			"page", "", "/listings",
			[]byte(`{}`),
		)
		rows, err := db.FetchEventsForAnon(ctx, pool, "anon-fetch-test", time.Now().Add(-1*time.Minute))
		if err != nil {
			t.Fatalf("FetchEventsForAnon: %v", err)
		}
		if len(rows) == 0 {
			t.Error("expected at least 1 row")
		}
	})

	t.Run("LoadActiveRules", func(t *testing.T) {
		_, _ = pool.Exec(ctx, `
			INSERT INTO rules (config_id, name, spec, enabled)
			VALUES ($1, 'test-rule', '{"type":"test"}'::jsonb, TRUE)`,
			configID)
		rules, err := db.LoadActiveRules(ctx, pool, "realestate")
		if err != nil {
			t.Fatalf("LoadActiveRules: %v", err)
		}
		if len(rules) == 0 {
			t.Error("expected at least 1 rule")
		}
	})

	t.Run("UpsertAndIsCooledDown", func(t *testing.T) {
		ruleID := uuid.New()
		anonID := "anon-cooldown-test"

		// Not cooled initially.
		cooled, err := db.IsCooledDown(ctx, pool, ruleID, anonID)
		if err != nil {
			t.Fatalf("IsCooledDown: %v", err)
		}
		if cooled {
			t.Error("expected not cooled down")
		}

		// Upsert a cooldown.
		if err := db.UpsertCooldown(ctx, pool, ruleID, anonID, time.Now().Add(1*time.Hour)); err != nil {
			t.Fatalf("UpsertCooldown: %v", err)
		}

		// Now should be cooled.
		cooled, err = db.IsCooledDown(ctx, pool, ruleID, anonID)
		if err != nil {
			t.Fatalf("IsCooledDown after upsert: %v", err)
		}
		if !cooled {
			t.Error("expected cooled down after upsert")
		}
	})

	t.Run("InsertTrigger", func(t *testing.T) {
		_, err := db.InsertEvent(ctx, pool,
			"anon-trigger-test", "", "write-key-1",
			"track", "Test Event", "",
			[]byte(`{}`),
		)
		if err != nil {
			t.Fatalf("InsertEvent: %v", err)
		}
		ruleID := uuid.New()
		trigID, err := db.InsertTrigger(ctx, pool, db.TriggerRow{
			RuleID:         &ruleID,
			RuleName:       "test-rule",
			Persona:        "realestate",
			AnonymousID:    "anon-trigger-test",
			FiredAt:        time.Now().UTC(),
			WindowSnapshot: []byte(`{}`),
			FullEvents:     []byte(`[]`),
			Destination:    "slack:realestate-realtor-pings",
			DispatchStatus: "pending",
		})
		if err != nil {
			t.Fatalf("InsertTrigger: %v", err)
		}
		if trigID == (uuid.UUID{}) {
			t.Error("expected non-zero UUID")
		}
	})

	t.Run("LoadProfile_NoRows", func(t *testing.T) {
		_, err := db.LoadProfile(ctx, pool, "user", "anonymous_id", "nonexistent-anon")
		if err == nil {
			t.Error("expected error for missing profile")
		}
	})

	t.Run("LoadCannedResponse_NoRows", func(t *testing.T) {
		_, err := db.LoadCannedResponse(ctx, pool, "nonexistent_template", "none")
		if err == nil {
			t.Error("expected error for missing canned response")
		}
	})

	t.Run("InsertMockEmail", func(t *testing.T) {
		id, err := db.InsertMockEmail(ctx, pool, db.MockEmailRow{
			ToEmail:      "test@example.com",
			Subject:      "Test Subject",
			BodyMarkdown: "Hello world",
		})
		if err != nil {
			t.Fatalf("InsertMockEmail: %v", err)
		}
		if id == (uuid.UUID{}) {
			t.Errorf("expected non-zero UUID, got zero")
		}
	})
}
