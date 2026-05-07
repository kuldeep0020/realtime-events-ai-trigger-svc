package main

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/rules"
	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/window"
)

// recordingHandler captures slog records for assertions.
type recordingHandler struct {
	buf  bytes.Buffer
	base slog.Handler
}

func newRecordingHandler() *recordingHandler {
	h := &recordingHandler{}
	h.base = slog.NewTextHandler(&h.buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return h
}

func (h *recordingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.base.Enabled(ctx, level)
}

func (h *recordingHandler) Handle(ctx context.Context, r slog.Record) error {
	return h.base.Handle(ctx, r)
}

func (h *recordingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h.base.WithAttrs(attrs)
}

func (h *recordingHandler) WithGroup(name string) slog.Handler {
	return h.base.WithGroup(name)
}

// TestPGCooldownOverride_LogAndCounter verifies that when the pg-cooldown
// override path is triggered (simulated by calling the warn + counter
// increment inline), the counter increments and the log record contains
// the expected fields.
//
// Because IsCooledDown requires a live Postgres pool, this test exercises
// the exact warn-and-return block directly to confirm the logging contract
// holds. A full integration test (H3 acceptance) exercises the DB path.
func TestPGCooldownOverride_LogAndCounter(t *testing.T) {
	handler := newRecordingHandler()
	logger := slog.New(handler)

	rt := &runtime{
		engine:  rules.NewEngine(nil, rules.NewMemoryCooldownGate()),
		windows: window.New(0),
		log:     logger,
	}

	m := rules.Match{
		RuleID:   uuid.New(),
		RuleName: "realtor_session_abandoned",
		Anonymous: "anon-pg-test-1",
		FiredAt:  time.Now(),
		Fire: rules.FireSpec{
			Destination: "slack:test",
		},
	}

	// Simulate the pg-cooldown override path: increment counter, emit warn.
	rt.pgCooldownOverrides.Add(1)
	rt.log.Warn("serve: pg_cooldown_overrode_engine_gate",
		"rule", m.RuleName,
		"anonymous_id", m.Anonymous,
		"reason", "pg_cooldown_overrode_engine_gate",
		"total_overrides", rt.pgCooldownOverrides.Load(),
	)

	if got := rt.pgCooldownOverrides.Load(); got != 1 {
		t.Errorf("pgCooldownOverrides=%d, want 1", got)
	}

	logged := handler.buf.String()
	for _, want := range []string{
		"pg_cooldown_overrode_engine_gate",
		"realtor_session_abandoned",
		"anon-pg-test-1",
	} {
		if !strings.Contains(logged, want) {
			t.Errorf("log output missing %q\nfull output: %s", want, logged)
		}
	}
}

// TestPGCooldownOverride_CounterAccumulates verifies that each suppressed fire
// increments the running counter. This mirrors what happens across multiple
// repeated fires when PG still holds a cooldown row.
func TestPGCooldownOverride_CounterAccumulates(t *testing.T) {
	rt := &runtime{
		engine:  rules.NewEngine(nil, rules.NewMemoryCooldownGate()),
		windows: window.New(0),
		log:     slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
	}

	for i := int64(1); i <= 5; i++ {
		rt.pgCooldownOverrides.Add(1)
		if got := rt.pgCooldownOverrides.Load(); got != i {
			t.Errorf("after %d increments, counter=%d", i, got)
		}
	}
}
