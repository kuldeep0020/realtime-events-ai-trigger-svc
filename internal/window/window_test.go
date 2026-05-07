package window

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/event"
)

// makeEvent is a tiny helper for synthesising events in tests.
func makeEvent(t *testing.T, typ, name, anonID, path string, props map[string]any, ts time.Time) *event.Event {
	t.Helper()
	var rawProps json.RawMessage
	if props != nil {
		b, err := json.Marshal(props)
		if err != nil {
			t.Fatalf("marshal props: %v", err)
		}
		rawProps = b
	}
	e := &event.Event{
		Type:              typ,
		Channel:           "browser",
		Event:             name,
		AnonymousID:       anonID,
		MessageID:         fmt.Sprintf("msg-%d", ts.UnixNano()),
		OriginalTimestamp: ts,
	}
	e.Context.Page.Path = path
	if rawProps != nil {
		e.Properties = rawProps
	}
	return e
}

func TestStoreShardDistribution(t *testing.T) {
	s := New(0)
	rng := rand.New(rand.NewSource(42))
	counts := map[*shard]int{}
	for i := 0; i < 1000; i++ {
		anonID := fmt.Sprintf("anon-%d-%d", rng.Int63(), i)
		sh := s.shardFor(anonID)
		counts[sh]++
	}
	if len(counts) != shardCount {
		t.Fatalf("expected all %d shards used, got %d", shardCount, len(counts))
	}
	// Spread check — at minimum every shard sees > 25 of 1000 (would expect
	// ~62 per shard with uniform distribution).
	for sh, n := range counts {
		if n < 25 {
			t.Errorf("shard %p underutilized: %d", sh, n)
		}
	}
}

func TestStoreSameAnonIDSameShard(t *testing.T) {
	s := New(0)
	id := "anon_demo-re-001"
	sh1 := s.shardFor(id)
	sh2 := s.shardFor(id)
	if sh1 != sh2 {
		t.Fatalf("shard mapping must be stable for same id")
	}
}

// TestRealEstateSequence fires the §6.2 demo event sequence and asserts the
// resulting window aggregations match what the rules engine will need.
func TestRealEstateSequence(t *testing.T) {
	s := New(0)
	const anonID = "anon_demo-re-001"
	t0 := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)

	// t=0 identify
	s.Update(&event.Event{
		Type:              "identify",
		AnonymousID:       anonID,
		MessageID:         "m0",
		OriginalTimestamp: t0,
		Traits:            json.RawMessage(`{"membership_tier":"browse"}`),
	}, time.Time{})

	// t=2 page /listings
	s.Update(makeEvent(t, "page", "", anonID, "/listings", nil, t0.Add(2*time.Second)), time.Time{})

	// t=5 track Listing Viewed L101
	s.Update(makeEvent(t, "track", "Listing Viewed", anonID, "/listings",
		map[string]any{"listing_id": "L101", "suburb": "suburb-1", "price": 1200000.0, "bedrooms": 3, "sq_ft": 2100, "view_count": 50},
		t0.Add(5*time.Second)), time.Time{})

	// t=9 track Filter Applied
	s.Update(makeEvent(t, "track", "Filter Applied", anonID, "/listings",
		map[string]any{"beds_min": 3, "price_min": 1000000, "price_max": 1800000, "results_count": 24},
		t0.Add(9*time.Second)), time.Time{})

	// t=13 Listing Viewed L107 (price=1500000 — new max)
	s.Update(makeEvent(t, "track", "Listing Viewed", anonID, "/listings",
		map[string]any{"listing_id": "L107", "suburb": "suburb-1", "price": 1500000.0, "bedrooms": 4, "sq_ft": 2400},
		t0.Add(13*time.Second)), time.Time{})

	// t=17 Listing Viewed L112 (price=1350000)
	s.Update(makeEvent(t, "track", "Listing Viewed", anonID, "/listings",
		map[string]any{"listing_id": "L112", "suburb": "suburb-1", "price": 1350000.0, "bedrooms": 3, "sq_ft": 2200, "view_count": 125, "listed_days_ago": 4},
		t0.Add(17*time.Second)), time.Time{})

	// t=20 page /listings/L112
	s.Update(makeEvent(t, "page", "", anonID, "/listings/L112", nil, t0.Add(20*time.Second)), time.Time{})

	snap, ok := s.Snapshot(anonID)
	if !ok {
		t.Fatalf("snapshot missing for %s", anonID)
	}
	if snap.EventCount != 7 {
		t.Errorf("EventCount = %d, want 7", snap.EventCount)
	}
	if snap.EventTypeCount["page"] != 2 {
		t.Errorf("page count = %d, want 2", snap.EventTypeCount["page"])
	}
	if snap.EventTypeCount["track"] != 4 {
		t.Errorf("track count = %d, want 4", snap.EventTypeCount["track"])
	}
	if snap.EventTypeCount["identify"] != 1 {
		t.Errorf("identify count = %d, want 1", snap.EventTypeCount["identify"])
	}
	if snap.EventNameCount["Listing Viewed"] != 3 {
		t.Errorf("Listing Viewed count = %d, want 3", snap.EventNameCount["Listing Viewed"])
	}
	if snap.EventNameCount["Filter Applied"] != 1 {
		t.Errorf("Filter Applied count = %d, want 1", snap.EventNameCount["Filter Applied"])
	}
	if snap.PathLatest != "/listings/L112" {
		t.Errorf("PathLatest = %q, want /listings/L112", snap.PathLatest)
	}
	if snap.DistinctPaths["/listings"] != 5 {
		// page /listings (1) + 3 Listing Viewed events with path /listings (3)
		// + Filter Applied with path /listings (1) = 5
		t.Errorf("DistinctPaths /listings = %d, want 5", snap.DistinctPaths["/listings"])
	}
	if snap.DistinctPaths["/listings/L112"] != 1 {
		t.Errorf("DistinctPaths /listings/L112 = %d, want 1", snap.DistinctPaths["/listings/L112"])
	}
	if snap.PropertyMaxNum["price"] != 1500000 {
		t.Errorf("PropertyMaxNum[price] = %v, want 1500000", snap.PropertyMaxNum["price"])
	}
	if snap.PropertyMaxNum["bedrooms"] != 4 {
		t.Errorf("PropertyMaxNum[bedrooms] = %v, want 4", snap.PropertyMaxNum["bedrooms"])
	}
	if snap.HasErrorEvent {
		t.Errorf("HasErrorEvent should be false for real-estate sequence")
	}
	if snap.Traits["membership_tier"] != "browse" {
		t.Errorf("Traits.membership_tier = %v, want browse", snap.Traits["membership_tier"])
	}
	if !snap.LastSeen.Equal(t0.Add(20 * time.Second)) {
		t.Errorf("LastSeen = %v, want %v", snap.LastSeen, t0.Add(20*time.Second))
	}
	if !snap.FirstSeen.Equal(t0) {
		t.Errorf("FirstSeen = %v, want %v", snap.FirstSeen, t0)
	}
}

// TestRSSelfErrorEvent fires the §6.3 sequence and asserts HasErrorEvent /
// LastErrorEvent are set correctly.
func TestRSSelfErrorEvent(t *testing.T) {
	s := New(0)
	const anonID = "demo-rs-001"
	t0 := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)

	s.Update(&event.Event{
		Type:              "identify",
		AnonymousID:       anonID,
		UserID:            "demo-rs-001",
		MessageID:         "m0",
		OriginalTimestamp: t0,
		Traits:            json.RawMessage(`{"plan":"free","company":"Acme"}`),
	}, time.Time{})
	s.Update(makeEvent(t, "track", "Account Created", anonID, "", map[string]any{"plan": "free"}, t0.Add(3*time.Second)), time.Time{})
	s.Update(makeEvent(t, "track", "Source Created", anonID, "", map[string]any{"source_type": "javascript", "elapsed_seconds_in_setup": 87}, t0.Add(6*time.Second)), time.Time{})
	s.Update(makeEvent(t, "track", "Destination Setup Error", anonID, "",
		map[string]any{"destination_type": "Amplitude", "step": "credentials_validation", "error_code": "AMP_INVALID_API_KEY", "error_message": "Provided API key was rejected", "elapsed_seconds_in_step": 134},
		t0.Add(10*time.Second)), time.Time{})

	snap, ok := s.Snapshot(anonID)
	if !ok {
		t.Fatalf("snapshot missing")
	}
	if !snap.HasErrorEvent {
		t.Fatalf("HasErrorEvent should be true after Destination Setup Error")
	}
	if snap.LastErrorEvent.EventName != "Destination Setup Error" {
		t.Errorf("LastErrorEvent.EventName = %q, want Destination Setup Error", snap.LastErrorEvent.EventName)
	}
	if snap.LastErrorEvent.Properties["error_code"] != "AMP_INVALID_API_KEY" {
		t.Errorf("LastErrorEvent.Properties[error_code] = %v, want AMP_INVALID_API_KEY", snap.LastErrorEvent.Properties["error_code"])
	}
	if snap.UserID != "demo-rs-001" {
		t.Errorf("UserID = %q, want demo-rs-001", snap.UserID)
	}
	if snap.Traits["plan"] != "free" {
		t.Errorf("Traits.plan = %v, want free", snap.Traits["plan"])
	}
}

func TestSnapshotIsDeepCopy(t *testing.T) {
	s := New(0)
	const anonID = "anon-deep-copy"
	t0 := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	s.Update(makeEvent(t, "track", "Listing Viewed", anonID, "/listings", map[string]any{"price": 1000.0}, t0), time.Time{})

	// Seed identify with a nested map and slice trait — these are the
	// shapes JSON unmarshalling produces from real payloads.
	s.Update(&event.Event{
		Type:              "identify",
		AnonymousID:       anonID,
		MessageID:         "id1",
		OriginalTimestamp: t0,
		Traits:            json.RawMessage(`{"tech_stack":["javascript","next.js"],"preferences":{"beds":3,"suburbs":["s1","s2"]}}`),
	}, time.Time{})

	snap1, _ := s.Snapshot(anonID)
	if snap1.EventNameCount["Listing Viewed"] != 1 {
		t.Fatalf("setup mismatch")
	}
	// Mutate top-level maps.
	snap1.EventNameCount["Listing Viewed"] = 999
	snap1.PropertyMaxNum["price"] = -1
	snap1.PropertyLast["price"] = "tampered"
	// Mutate nested slice and map values — the most subtle deep-copy hazard.
	if stack, ok := snap1.Traits["tech_stack"].([]any); ok {
		stack[0] = "TAMPERED"
	} else {
		t.Fatalf("expected tech_stack to be []any, got %T", snap1.Traits["tech_stack"])
	}
	if prefs, ok := snap1.Traits["preferences"].(map[string]any); ok {
		prefs["beds"] = 999.0
		if subs, ok := prefs["suburbs"].([]any); ok {
			subs[0] = "TAMPERED"
		}
	} else {
		t.Fatalf("expected preferences to be map[string]any, got %T", snap1.Traits["preferences"])
	}

	snap2, _ := s.Snapshot(anonID)
	if snap2.EventNameCount["Listing Viewed"] != 1 {
		t.Errorf("snapshot maps shared between calls (got %d)", snap2.EventNameCount["Listing Viewed"])
	}
	if snap2.PropertyMaxNum["price"] != 1000 {
		t.Errorf("PropertyMaxNum shared: got %v", snap2.PropertyMaxNum["price"])
	}
	if snap2.PropertyLast["price"] != 1000.0 {
		t.Errorf("PropertyLast shared: got %v", snap2.PropertyLast["price"])
	}
	// Nested slice must be untouched in window state.
	if stack, _ := snap2.Traits["tech_stack"].([]any); stack[0] != "javascript" {
		t.Errorf("nested slice shared: got %v", stack)
	}
	// Nested map must be untouched in window state.
	prefs, _ := snap2.Traits["preferences"].(map[string]any)
	if prefs["beds"] != 3.0 {
		t.Errorf("nested map shared: got %v", prefs["beds"])
	}
	if subs, _ := prefs["suburbs"].([]any); subs[0] != "s1" {
		t.Errorf("doubly-nested slice shared: got %v", subs)
	}
}

func TestScanIdleReturnsOnlyStale(t *testing.T) {
	s := New(0)
	now := time.Now().UTC()
	// fresh window (LastSeen now)
	s.Update(makeEvent(t, "track", "Recent", "fresh", "", nil, now), time.Time{})
	// stale window (LastSeen 30s ago)
	s.Update(makeEvent(t, "track", "Old", "stale", "", nil, now.Add(-30*time.Second)), time.Time{})

	var seen []string
	s.ScanIdle(10*time.Second, func(snap Snapshot) {
		seen = append(seen, snap.AnonymousID)
	})
	if len(seen) != 1 || seen[0] != "stale" {
		t.Errorf("ScanIdle returned %v, want [stale]", seen)
	}
}

func TestPruneRemovesOldWindows(t *testing.T) {
	s := New(0)
	now := time.Now().UTC()
	s.Update(makeEvent(t, "track", "Recent", "fresh", "", nil, now), time.Time{})
	s.Update(makeEvent(t, "track", "Old", "stale", "", nil, now.Add(-time.Hour)), time.Time{})

	if got := s.Active(); got != 2 {
		t.Fatalf("Active = %d, want 2", got)
	}
	removed := s.Prune(10 * time.Minute)
	if removed != 1 {
		t.Errorf("Prune removed %d, want 1", removed)
	}
	if got := s.Active(); got != 1 {
		t.Errorf("Active after prune = %d, want 1", got)
	}
	if _, ok := s.Snapshot("stale"); ok {
		t.Errorf("stale window should be gone")
	}
	if _, ok := s.Snapshot("fresh"); !ok {
		t.Errorf("fresh window should remain")
	}

	// Drain prunedCh to confirm notification fired.
	select {
	case id := <-s.PrunedChan():
		if id != "stale" {
			t.Errorf("pruned id = %q, want stale", id)
		}
	case <-time.After(time.Second):
		t.Errorf("expected pruned notification")
	}
}

func TestLRUEviction(t *testing.T) {
	// Cap at 3 globally. With 16 shards, we'll see eviction even though
	// shard-local: filling beyond 3 across any path triggers it.
	s := New(3)
	// Insert 5 windows; the count must never exceed 3 by much (soft cap).
	for i := 0; i < 5; i++ {
		anonID := fmt.Sprintf("anon-lru-%d", i)
		s.Update(makeEvent(t, "track", "x", anonID, "", nil, time.Now().UTC()), time.Time{})
	}
	if s.Active() > 3 {
		t.Errorf("Active = %d, want <= 3 (soft cap)", s.Active())
	}
}

func TestRunPrunerLifecycle(t *testing.T) {
	s := New(0)
	now := time.Now().UTC()
	s.Update(makeEvent(t, "track", "Old", "stale", "", nil, now.Add(-time.Hour)), time.Time{})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.RunPruner(ctx, 10*time.Minute, 20*time.Millisecond)
		close(done)
	}()
	// Wait for pruning to occur — drain pruned channel.
	select {
	case <-s.PrunedChan():
	case <-time.After(time.Second):
		t.Fatalf("pruner didn't fire within 1s")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("pruner didn't exit on cancel")
	}
}

// TestConcurrentWithWindow exercises the shard locks under contention. Run
// with -race to catch any data races.
func TestConcurrentWithWindow(t *testing.T) {
	s := New(0)
	const workers = 32
	const perWorker = 200
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func(w int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(w)))
			for i := 0; i < perWorker; i++ {
				// 5 hot keys to ensure contention on a single shard.
				anonID := fmt.Sprintf("anon-%d", rng.Intn(5))
				ts := time.Unix(1700000000, int64(i)*int64(time.Millisecond))
				s.Update(makeEvent(t, "track", "Listing Viewed", anonID, "/listings", map[string]any{"price": float64(1000 + i)}, ts), time.Time{})
				if i%10 == 0 {
					_, _ = s.Snapshot(anonID)
				}
				if i%50 == 0 {
					s.ScanIdle(0, func(_ Snapshot) {})
				}
			}
		}(w)
	}
	wg.Wait()

	// Total events must equal workers * perWorker.
	total := 0
	for i := 0; i < 5; i++ {
		anonID := fmt.Sprintf("anon-%d", i)
		if snap, ok := s.Snapshot(anonID); ok {
			total += snap.EventCount
		}
	}
	if total != workers*perWorker {
		t.Errorf("total events = %d, want %d", total, workers*perWorker)
	}
}

func TestCooldownTracking(t *testing.T) {
	s := New(0)
	const anonID = "anon-cd"
	t0 := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	s.Update(makeEvent(t, "track", "x", anonID, "", nil, t0), time.Time{})

	s.WithWindow(anonID, func(w *UserWindow) {
		w.MarkTriggered("rule-A", t0)
	})
	s.WithWindow(anonID, func(w *UserWindow) {
		if !w.CooldownActive("rule-A", time.Hour, t0.Add(30*time.Minute)) {
			t.Errorf("rule-A should be on cooldown at t0+30m")
		}
		if w.CooldownActive("rule-A", time.Hour, t0.Add(2*time.Hour)) {
			t.Errorf("rule-A should be off cooldown at t0+2h")
		}
		if w.CooldownActive("rule-B", time.Hour, t0) {
			t.Errorf("rule-B never fired; cooldown should be inactive")
		}
	})

	snap, _ := s.Snapshot(anonID)
	if _, ok := snap.TriggeredRules["rule-A"]; !ok {
		t.Errorf("snapshot should expose triggered rules")
	}
	// Mutating snapshot.TriggeredRules must not affect the underlying window.
	snap.TriggeredRules["rule-A"] = time.Time{}
	s.WithWindow(anonID, func(w *UserWindow) {
		if w.triggeredRules["rule-A"].IsZero() {
			t.Errorf("snapshot.TriggeredRules is not isolated from internal map")
		}
	})
}

// TestSSEStream_TriggerWindowSnapshotIdleSeconds verifies that snapshots
// produced by ScanIdle carry a non-zero IdleSeconds value. This is the
// regression test for the bug where ScanIdle omitted the IdleSeconds
// population step that Store.Snapshot performs, causing the TriggerCard
// "Why" panel to display "Idle: 0s" even when an idle-time rule had just
// fired.
func TestSSEStream_TriggerWindowSnapshotIdleSeconds(t *testing.T) {
	s := New(0)
	// Place LastSeen 12 seconds in the past so ScanIdle(1s, …) includes it.
	lastSeen := time.Now().UTC().Add(-12 * time.Second)
	s.WithWindow("anon-idle-test", func(w *UserWindow) {
		w.LastSeen = lastSeen
	})

	var captured []Snapshot
	s.ScanIdle(time.Second, func(snap Snapshot) {
		captured = append(captured, snap)
	})

	if len(captured) != 1 {
		t.Fatalf("expected 1 snapshot from ScanIdle, got %d", len(captured))
	}
	snap := captured[0]
	if snap.IdleSeconds < 10 {
		t.Errorf("IdleSeconds = %d, want >= 10 (LastSeen was 12s ago)", snap.IdleSeconds)
	}
}

// TestApply_LastSeenUsesReceivedAtNotOriginalTimestamp verifies that apply uses
// the supplied receivedAt as the authoritative clock for LastSeen / FirstSeen,
// not the (potentially stale) OriginalTimestamp on the event. This is the
// regression test for the demo-script bug where all 8 events share an identical
// OriginalTimestamp stamped at script-build-time, causing the idle ticker to
// fire prematurely because LastSeen never advanced.
func TestApply_LastSeenUsesReceivedAtNotOriginalTimestamp(t *testing.T) {
	s := New(0)
	const anonID = "anon-received-at-test"

	// Simulate a stale event: OriginalTimestamp is 1 hour in the past, but
	// receivedAt is now (the server dequeued it just now).
	staleOriginal := time.Now().UTC().Add(-1 * time.Hour)
	receivedAt := time.Now().UTC()

	s.Update(&event.Event{
		Type:              "track",
		Event:             "Listing Viewed",
		AnonymousID:       anonID,
		MessageID:         "msg-stale",
		OriginalTimestamp: staleOriginal,
	}, receivedAt)

	snap, ok := s.Snapshot(anonID)
	if !ok {
		t.Fatalf("snapshot missing for %s", anonID)
	}

	// LastSeen must be receivedAt, not staleOriginal.
	// We allow a 1-second tolerance for clock jitter.
	diff := snap.LastSeen.Sub(receivedAt)
	if diff < -time.Second || diff > time.Second {
		t.Errorf("LastSeen = %v, want approx receivedAt=%v (diff=%v); stale OriginalTimestamp=%v must NOT be used",
			snap.LastSeen, receivedAt, diff, staleOriginal)
	}

	// FirstSeen must also be receivedAt, not staleOriginal.
	diff2 := snap.FirstSeen.Sub(receivedAt)
	if diff2 < -time.Second || diff2 > time.Second {
		t.Errorf("FirstSeen = %v, want approx receivedAt=%v (diff=%v); stale OriginalTimestamp=%v must NOT be used",
			snap.FirstSeen, receivedAt, diff2, staleOriginal)
	}
}

// TestSnapshotAll_ReturnsAllWindows verifies that SnapshotAll returns one
// snapshot per inserted window with IdleSeconds populated.
func TestSnapshotAll_ReturnsAllWindows(t *testing.T) {
	s := New(0)
	now := time.Now().UTC()

	// Insert 3 windows with known LastSeen values across potentially different shards.
	ids := []string{"anon-all-001", "anon-all-002", "anon-all-003"}
	for _, id := range ids {
		s.WithWindow(id, func(w *UserWindow) {
			w.EventCount = 1
			w.LastSeen = now.Add(-5 * time.Second)
		})
	}

	snaps := s.SnapshotAll()
	if len(snaps) != 3 {
		t.Fatalf("SnapshotAll returned %d snapshots, want 3", len(snaps))
	}

	seen := map[string]bool{}
	for _, snap := range snaps {
		seen[snap.AnonymousID] = true
		// IdleSeconds must be populated (LastSeen was ~5s ago).
		if snap.IdleSeconds < 3 {
			t.Errorf("snap %s: IdleSeconds = %d, want >= 3 (LastSeen was ~5s ago)", snap.AnonymousID, snap.IdleSeconds)
		}
	}

	for _, id := range ids {
		if !seen[id] {
			t.Errorf("SnapshotAll missing window for %s", id)
		}
	}
}

func TestUpdateNilEventNoop(t *testing.T) {
	s := New(0)
	s.Update(nil, time.Time{})
	if s.Active() != 0 {
		t.Errorf("nil update should not create windows")
	}
}

func TestUpdateEmptyAnonID(t *testing.T) {
	s := New(0)
	s.Update(&event.Event{Type: "track", Event: "x"}, time.Time{})
	if s.Active() != 0 {
		t.Errorf("event without anonymousId/userId should be skipped")
	}
}

func TestSnapshotMissingReturnsFalse(t *testing.T) {
	s := New(0)
	if _, ok := s.Snapshot("nope"); ok {
		t.Errorf("missing anonID should return ok=false")
	}
}

func TestHasErrorSuffix(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"Destination Setup Error", true},
		{"Source Setup Error", true},
		{"Listing Viewed", false},
		{"Error", true},
		{"", false},
	}
	for _, c := range cases {
		if got := hasErrorSuffix(c.in); got != c.want {
			t.Errorf("hasErrorSuffix(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
