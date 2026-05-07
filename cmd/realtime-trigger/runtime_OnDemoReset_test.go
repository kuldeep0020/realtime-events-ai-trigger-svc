package main

import (
	"context"
	"testing"
	"time"

	"log/slog"
	"os"

	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/event"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/rules"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/window"
)

// TestOnDemoReset_PurgesMemoryState verifies that OnDemoReset clears both the
// in-memory cooldown gate and the window store, leaving both empty regardless
// of prior activity.
func TestOnDemoReset_PurgesMemoryState(t *testing.T) {
	gate := rules.NewMemoryCooldownGate()
	engine := rules.NewEngine(nil, gate)
	store := window.New(0)
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	rt := &runtime{
		engine:  engine,
		windows: store,
		log:     log,
	}

	// Seed some cooldown entries in the gate.
	gate.Mark("anon-1", "realtor_session_abandoned", time.Now(), time.Hour)
	gate.Mark("anon-2", "onboarding_errored", time.Now(), 24*time.Hour)
	if gate.Size() != 2 {
		t.Fatalf("expected 2 gate entries before reset, got %d", gate.Size())
	}

	// Seed a couple of windows.
	store.Update(&event.Event{
		Type:              "page",
		AnonymousID:       "anon-1",
		MessageID:         "m1",
		OriginalTimestamp: time.Now(),
	})
	store.Update(&event.Event{
		Type:              "track",
		AnonymousID:       "anon-2",
		MessageID:         "m2",
		OriginalTimestamp: time.Now(),
	})
	if store.Active() != 2 {
		t.Fatalf("expected 2 windows before reset, got %d", store.Active())
	}

	// Call OnDemoReset.
	cooldownsCleared, windowsCleared, err := rt.OnDemoReset(context.Background())
	if err != nil {
		t.Fatalf("OnDemoReset returned error: %v", err)
	}

	// Both counts must reflect what was present.
	if cooldownsCleared != 2 {
		t.Errorf("cooldownsCleared=%d, want 2", cooldownsCleared)
	}
	if windowsCleared != 2 {
		t.Errorf("windowsCleared=%d, want 2", windowsCleared)
	}

	// Both in-memory stores must now be empty.
	if n := gate.Size(); n != 0 {
		t.Errorf("gate still has %d entries after reset", n)
	}
	if n := store.Active(); n != 0 {
		t.Errorf("window store still has %d windows after reset", n)
	}
}

// TestOnDemoReset_IdempotentOnEmpty verifies OnDemoReset is a no-op (returns
// zeros, no error) when both stores are already empty.
func TestOnDemoReset_IdempotentOnEmpty(t *testing.T) {
	rt := &runtime{
		engine:  rules.NewEngine(nil, rules.NewMemoryCooldownGate()),
		windows: window.New(0),
		log:     slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}

	cooldownsCleared, windowsCleared, err := rt.OnDemoReset(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cooldownsCleared != 0 || windowsCleared != 0 {
		t.Errorf("expected (0,0) on empty state, got (%d,%d)", cooldownsCleared, windowsCleared)
	}
}
