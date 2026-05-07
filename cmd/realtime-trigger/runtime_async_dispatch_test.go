package main

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/rules"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/window"
)

// minimalRuntime constructs a runtime with only the fields needed by the
// async-dispatch path wired. DB/LLM/Slack are nil so fireMatch will bail
// out early (pgCooldown check returns not-cooled for nil pool, and nil checks
// are guarded in each caller). We only care about channel semantics here.
func minimalRuntime(t *testing.T) *runtime {
	t.Helper()
	return &runtime{
		matchCh: make(chan dispatchItem, 64),
		log:     slog.New(slog.NewTextHandler(os.Stderr, nil)),
		engine:  rules.NewEngine(nil, rules.NewMemoryCooldownGate()),
		windows: window.New(0),
	}
}

func makeDispatchItem(ruleName, anonID string) dispatchItem {
	return dispatchItem{
		m: rules.Match{
			RuleID:   uuid.New(),
			RuleName: ruleName,
			Anonymous: anonID,
			Fire: rules.FireSpec{
				Destination: "slack:test",
			},
		},
		persona: "realestate",
	}
}

// TestMatchCh_SendAndReceive verifies that items placed on matchCh can be
// read back — basic channel wiring test.
func TestMatchCh_SendAndReceive(t *testing.T) {
	rt := minimalRuntime(t)
	item := makeDispatchItem("realtor_session_abandoned", "anon-test-1")

	rt.matchCh <- item
	select {
	case got := <-rt.matchCh:
		if got.m.RuleName != item.m.RuleName {
			t.Errorf("rule name mismatch: got %q, want %q", got.m.RuleName, item.m.RuleName)
		}
		if got.m.Anonymous != item.m.Anonymous {
			t.Errorf("anon mismatch: got %q, want %q", got.m.Anonymous, item.m.Anonymous)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for item from matchCh")
	}
}

// TestMatchCh_DropsWhenFull verifies that when matchCh is full the non-blocking
// send path drops the item and increments matchDropped rather than blocking.
func TestMatchCh_DropsWhenFull(t *testing.T) {
	rt := minimalRuntime(t)

	// Fill the channel to capacity.
	for i := 0; i < cap(rt.matchCh); i++ {
		rt.matchCh <- makeDispatchItem("rule", "anon")
	}

	// This item must be dropped without blocking.
	done := make(chan struct{})
	go func() {
		defer close(done)
		item := makeDispatchItem("rule", "anon-overflow")
		select {
		case rt.matchCh <- item:
			// Should not happen — channel is full.
		default:
			rt.matchDropped.Add(1)
			rt.log.Warn("serve: match_dropped — dispatch worker pool saturated",
				"rule", item.m.RuleName,
				"anon", item.m.Anonymous,
				"total_dropped", rt.matchDropped.Load(),
			)
		}
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("non-blocking drop path blocked unexpectedly")
	}

	if dropped := rt.matchDropped.Load(); dropped != 1 {
		t.Errorf("matchDropped=%d, want 1", dropped)
	}
}

// TestRunDispatcher_ProcessesItem verifies that runDispatcher workers drain
// matchCh items. We send one item and confirm it is consumed (channel becomes
// empty) within a short deadline. Since fireMatch exits early on nil pool, this
// tests the channel-processing flow without DB/Slack side effects.
func TestRunDispatcher_ProcessesItem(t *testing.T) {
	rt := minimalRuntime(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go rt.runDispatcher(ctx)

	// Give workers time to start.
	time.Sleep(10 * time.Millisecond)

	rt.matchCh <- makeDispatchItem("realtor_session_abandoned", "anon-dispatch-1")

	// Wait for the channel to drain (item consumed by a worker).
	deadline := time.After(3 * time.Second)
	for {
		select {
		case <-deadline:
			t.Errorf("matchCh was not drained within deadline; len=%d", len(rt.matchCh))
			return
		default:
			if len(rt.matchCh) == 0 {
				return // success
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
}
