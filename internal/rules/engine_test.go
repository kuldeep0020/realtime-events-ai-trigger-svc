package rules

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/event"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/window"
)

// realestateConfigYAML mirrors §5.1 of the design doc.
const realestateConfigYAML = `
persona: realestate
realtors:
  - {name: "Priya N.",  suburbs: ["suburb-1", "suburb-2"], hours: "09:00-18:00 IST"}
  - {name: "Arjun M.",  suburbs: ["suburb-3"],             hours: "10:00-19:00 IST"}
slack_channel: "realestate-realtor-pings"

rules:
  - name: realtor_session_abandoned
    when:
      all:
        - window.event_count: { ">=": 3 }
        - window.event_path_matches: "^/listings(/|$).*"
        - window.idle_seconds: { ">=": 10 }
        - window.has_event_type: page
    fire:
      action_template: realestate_realtor_pitch
      destination: "slack:realestate-realtor-pings"
      cooldown_seconds: 3600
`

// rsSelfConfigYAML mirrors §5.2 of the design doc.
const rsSelfConfigYAML = `
persona: rs-self
rules:
  - name: onboarding_errored
    when:
      any:
        - window.has_event_name: "Source Setup Error"
        - window.has_event_name: "Destination Setup Error"
        - window.has_event_name: "Webhook Send Error"
    fire:
      action_template: rs_onboarding_stuck
      destination: "email:user"
      cooldown_seconds: 86400

  - name: onboarding_stuck
    when:
      all:
        - window.has_event_name: "Source Created"
        - window.event_count_of_name: { name: "Destination Created", "==": 0 }
        - window.idle_seconds: { ">=": 15 }
    fire:
      action_template: rs_onboarding_stuck
      destination: "email:user"
      cooldown_seconds: 86400
`

func makeEvent(t *testing.T, typ, name, anonID, path string, props map[string]any, ts time.Time) *event.Event {
	t.Helper()
	var raw json.RawMessage
	if props != nil {
		b, err := json.Marshal(props)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		raw = b
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
	if raw != nil {
		e.Properties = raw
	}
	return e
}

// fireRealEstateSequence drives the §6.2 demo into a fresh window store and
// returns the snapshot at the post-idle moment (t = 32s after t0).
func fireRealEstateSequence(t *testing.T, store *window.Store, anonID string, t0 time.Time) {
	t.Helper()
	store.Update(&event.Event{
		Type:              "identify",
		AnonymousID:       anonID,
		MessageID:         "id0",
		OriginalTimestamp: t0,
		Traits:            json.RawMessage(`{"membership_tier":"browse"}`),
	})
	store.Update(makeEvent(t, "page", "", anonID, "/listings", nil, t0.Add(2*time.Second)))
	store.Update(makeEvent(t, "track", "Listing Viewed", anonID, "/listings",
		map[string]any{"listing_id": "L101", "suburb": "suburb-1", "price": 1200000.0, "bedrooms": 3, "sq_ft": 2100}, t0.Add(5*time.Second)))
	store.Update(makeEvent(t, "track", "Filter Applied", anonID, "/listings",
		map[string]any{"beds_min": 3, "price_min": 1000000, "price_max": 1800000, "results_count": 24}, t0.Add(9*time.Second)))
	store.Update(makeEvent(t, "track", "Listing Viewed", anonID, "/listings",
		map[string]any{"listing_id": "L107", "suburb": "suburb-1", "price": 1500000.0, "bedrooms": 4, "sq_ft": 2400}, t0.Add(13*time.Second)))
	store.Update(makeEvent(t, "track", "Listing Viewed", anonID, "/listings",
		map[string]any{"listing_id": "L112", "suburb": "suburb-1", "price": 1350000.0, "bedrooms": 3, "sq_ft": 2200}, t0.Add(17*time.Second)))
	store.Update(makeEvent(t, "page", "", anonID, "/listings/L112", nil, t0.Add(20*time.Second)))
}

func fireRSSelfSequence(t *testing.T, store *window.Store, anonID string, t0 time.Time, includeError bool) {
	t.Helper()
	store.Update(&event.Event{
		Type:              "identify",
		AnonymousID:       anonID,
		UserID:            "demo-rs-001",
		MessageID:         "id0",
		OriginalTimestamp: t0,
		Traits:            json.RawMessage(`{"plan":"free","company":"Acme"}`),
	})
	store.Update(makeEvent(t, "track", "Account Created", anonID, "", map[string]any{"plan": "free"}, t0.Add(3*time.Second)))
	store.Update(makeEvent(t, "track", "Source Created", anonID, "",
		map[string]any{"source_type": "javascript", "elapsed_seconds_in_setup": 87}, t0.Add(6*time.Second)))
	if includeError {
		store.Update(makeEvent(t, "track", "Destination Setup Error", anonID, "",
			map[string]any{"destination_type": "Amplitude", "step": "credentials_validation",
				"error_code": "AMP_INVALID_API_KEY", "error_message": "rejected", "elapsed_seconds_in_step": 134}, t0.Add(10*time.Second)))
	}
}

func TestLoadRealEstateConfig(t *testing.T) {
	cfg, rules, err := LoadPersonaConfigYAML([]byte(realestateConfigYAML))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Persona != "realestate" {
		t.Errorf("persona = %q, want realestate", cfg.Persona)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	r := rules[0]
	if r.Name != "realtor_session_abandoned" {
		t.Errorf("rule name = %q", r.Name)
	}
	if r.Cooldown != time.Hour {
		t.Errorf("cooldown = %v, want 1h", r.Cooldown)
	}
	if !r.When.UsesTime() {
		t.Errorf("rule should report UsesTime() = true (idle_seconds present)")
	}
}

func TestLoadRSSelfConfig(t *testing.T) {
	cfg, rules, err := LoadPersonaConfigYAML([]byte(rsSelfConfigYAML))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Persona != "rs-self" {
		t.Errorf("persona = %q, want rs-self", cfg.Persona)
	}
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	if rules[0].When.UsesTime() {
		t.Errorf("onboarding_errored should NOT use time")
	}
	if !rules[1].When.UsesTime() {
		t.Errorf("onboarding_stuck SHOULD use time (idle_seconds)")
	}
}

// TestRealEstateRuleFires drives the §6.2 sequence into a window store, loads
// the §5.1 config, and asserts the realtor_session_abandoned rule fires only
// after 10s of idle time.
func TestRealEstateRuleFires(t *testing.T) {
	_, rules, err := LoadPersonaConfigYAML([]byte(realestateConfigYAML))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	store := window.New(0)
	gate := NewMemoryCooldownGate()
	engine := NewEngine(rules, gate)

	const anonID = "anon_demo-re-001"
	t0 := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	fireRealEstateSequence(t, store, anonID, t0)

	snap, ok := store.Snapshot(anonID)
	if !ok {
		t.Fatalf("snapshot missing")
	}

	// At t0+22s — only 2s idle since t0+20s. Rule MUST NOT fire (idle_seconds < 10).
	atT22 := t0.Add(22 * time.Second)
	if matches := engine.EvaluateOnTick(snap, "realestate", atT22); len(matches) != 0 {
		t.Fatalf("rule fired too early at idle 2s: %v", matches)
	}

	// At t0+32s — 12s idle. Rule MUST fire.
	atT32 := t0.Add(32 * time.Second)
	matches := engine.EvaluateOnTick(snap, "realestate", atT32)
	if len(matches) != 1 {
		t.Fatalf("expected 1 match at idle 12s, got %d", len(matches))
	}
	m := matches[0]
	if m.RuleName != "realtor_session_abandoned" {
		t.Errorf("rule name = %q", m.RuleName)
	}
	if m.Anonymous != anonID {
		t.Errorf("anonymous = %q", m.Anonymous)
	}
	if m.Fire.Destination != "slack:realestate-realtor-pings" {
		t.Errorf("destination = %q", m.Fire.Destination)
	}
}

// TestRSSelfErrorRuleFiresImmediately drives the §6.3 sequence and asserts
// the onboarding_errored rule fires on the same event it depends on (no
// idle wait).
func TestRSSelfErrorRuleFiresImmediately(t *testing.T) {
	_, rules, err := LoadPersonaConfigYAML([]byte(rsSelfConfigYAML))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	store := window.New(0)
	gate := NewMemoryCooldownGate()
	engine := NewEngine(rules, gate)

	const anonID = "demo-rs-001"
	t0 := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	fireRSSelfSequence(t, store, anonID, t0, true)

	snap, ok := store.Snapshot(anonID)
	if !ok {
		t.Fatalf("snapshot missing")
	}

	// EvaluateOnEvent at t0+10s (the error event). Rule fires.
	atErr := t0.Add(10 * time.Second)
	matches := engine.EvaluateOnEvent(snap, "rs-self", atErr)
	if len(matches) != 1 {
		t.Fatalf("expected 1 match (onboarding_errored), got %d: %+v", len(matches), matches)
	}
	if matches[0].RuleName != "onboarding_errored" {
		t.Errorf("rule = %q, want onboarding_errored", matches[0].RuleName)
	}
}

// TestCooldownPreventsDoubleFire verifies that a second EvaluateOnEvent call
// returns no matches while the cooldown is active. We only target the
// onboarding_errored rule (single-rule config) so other rules in §5.2 don't
// conflate the cooldown check.
func TestCooldownPreventsDoubleFire(t *testing.T) {
	// Single-rule config so the cooldown semantics are unambiguous.
	cfg := []byte(`
persona: rs-self
rules:
  - name: onboarding_errored
    when:
      any:
        - window.has_event_name: "Destination Setup Error"
    fire:
      action_template: rs_onboarding_stuck
      destination: "email:user"
      cooldown_seconds: 86400
`)
	_, rules, err := LoadPersonaConfigYAML(cfg)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	store := window.New(0)
	gate := NewMemoryCooldownGate()
	engine := NewEngine(rules, gate)

	const anonID = "demo-rs-001"
	t0 := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	fireRSSelfSequence(t, store, anonID, t0, true)
	snap, _ := store.Snapshot(anonID)

	atErr := t0.Add(10 * time.Second)
	if got := engine.EvaluateOnEvent(snap, "rs-self", atErr); len(got) != 1 {
		t.Fatalf("first eval got %d, want 1", len(got))
	}
	// Same instant — must be gated.
	if got := engine.EvaluateOnEvent(snap, "rs-self", atErr); len(got) != 0 {
		t.Errorf("cooldown failed: second eval got %d", len(got))
	}
	// 1 hour later — still gated (cooldown 24h).
	if got := engine.EvaluateOnEvent(snap, "rs-self", atErr.Add(time.Hour)); len(got) != 0 {
		t.Errorf("cooldown failed at +1h: %d matches", len(got))
	}
	// 25 hours later — no longer gated.
	if got := engine.EvaluateOnEvent(snap, "rs-self", atErr.Add(25*time.Hour)); len(got) != 1 {
		t.Errorf("cooldown should expire at +25h: %d matches", len(got))
	}
}

// TestCooldownIsLastGate ensures that the cooldown gate is consulted AFTER
// predicate evaluation. We use a counting gate to verify Allow is called
// exactly when the predicate matches.
func TestCooldownIsLastGate(t *testing.T) {
	_, rules, err := LoadPersonaConfigYAML([]byte(rsSelfConfigYAML))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	gate := &countingGate{inner: NewMemoryCooldownGate()}
	engine := NewEngine(rules, gate)

	store := window.New(0)
	const anonID = "demo-rs-001"
	t0 := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	// Sequence WITHOUT the error event — predicate cannot match.
	fireRSSelfSequence(t, store, anonID, t0, false)
	snap, _ := store.Snapshot(anonID)

	atErr := t0.Add(10 * time.Second)
	matches := engine.EvaluateOnEvent(snap, "rs-self", atErr)
	if len(matches) != 0 {
		t.Errorf("expected 0 matches (no error event), got %d", len(matches))
	}
	if got := atomic.LoadInt32(&gate.allowCalls); got != 0 {
		t.Errorf("Allow should not be called when predicate fails: got %d calls", got)
	}

	// Now fire the error event; Allow should be consulted exactly once
	// (one matching rule).
	store.Update(makeEvent(t, "track", "Destination Setup Error", anonID, "",
		map[string]any{"error_code": "AMP_INVALID_API_KEY"}, atErr))
	snap2, _ := store.Snapshot(anonID)
	atomic.StoreInt32(&gate.allowCalls, 0)
	atomic.StoreInt32(&gate.markCalls, 0)
	matches = engine.EvaluateOnEvent(snap2, "rs-self", atErr)
	if len(matches) != 1 {
		t.Errorf("expected 1 match, got %d", len(matches))
	}
	if got := atomic.LoadInt32(&gate.allowCalls); got != 1 {
		t.Errorf("Allow should be called exactly once: got %d", got)
	}
	if got := atomic.LoadInt32(&gate.markCalls); got != 1 {
		t.Errorf("Mark should be called exactly once: got %d", got)
	}
}

// TestEvaluateOnTickSkipsTimeIndependentRules verifies that the tick path
// skips rules that don't depend on time (e.g. onboarding_errored).
func TestEvaluateOnTickSkipsTimeIndependentRules(t *testing.T) {
	_, rules, err := LoadPersonaConfigYAML([]byte(rsSelfConfigYAML))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	store := window.New(0)
	engine := NewEngine(rules, NewMemoryCooldownGate())

	const anonID = "demo-rs-001"
	t0 := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	fireRSSelfSequence(t, store, anonID, t0, true)
	snap, _ := store.Snapshot(anonID)

	// Tick path with the error event present: onboarding_errored does NOT
	// use time, so it must be skipped here.
	matches := engine.EvaluateOnTick(snap, "rs-self", t0.Add(time.Hour))
	for _, m := range matches {
		if m.RuleName == "onboarding_errored" {
			t.Errorf("onboarding_errored should NOT fire from tick path (no time dep)")
		}
	}
}

// TestHotReloadAtomicSwap fires concurrent evaluations while replacing the
// rule list. With the atomic pointer we should never see a torn read.
func TestHotReloadAtomicSwap(t *testing.T) {
	_, rules1, _ := LoadPersonaConfigYAML([]byte(rsSelfConfigYAML))
	_, rules2, _ := LoadPersonaConfigYAML([]byte(realestateConfigYAML))
	engine := NewEngine(rules1, NewMemoryCooldownGate())

	store := window.New(0)
	const anonID = "demo-rs-001"
	t0 := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	fireRSSelfSequence(t, store, anonID, t0, true)
	snap, _ := store.Snapshot(anonID)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(8)
	for i := 0; i < 8; i++ {
		go func() {
			defer wg.Done()
			now := t0.Add(time.Hour)
			for {
				select {
				case <-stop:
					return
				default:
					_ = engine.EvaluateOnEvent(snap, "rs-self", now)
					_ = engine.EvaluateOnEvent(snap, "realestate", now)
					_ = engine.EvaluateOnTick(snap, "rs-self", now)
				}
			}
		}()
	}
	// Hammer Replace from this goroutine.
	for i := 0; i < 200; i++ {
		if i%2 == 0 {
			engine.Replace(rules1)
		} else {
			engine.Replace(rules2)
		}
	}
	close(stop)
	wg.Wait()

	got := engine.Snapshot()
	if len(got) == 0 {
		t.Errorf("snapshot empty after replace")
	}
}

// TestReloadCallsLoader verifies Reload fetches and swaps.
func TestReloadCallsLoader(t *testing.T) {
	engine := NewEngine(nil, NewMemoryCooldownGate())
	calls := 0
	engine.SetLoader(func(_ context.Context) ([]Rule, error) {
		calls++
		_, rules, _ := LoadPersonaConfigYAML([]byte(rsSelfConfigYAML))
		return rules, nil
	})
	if err := engine.Reload(context.Background()); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 loader call, got %d", calls)
	}
	if len(engine.Snapshot()) != 2 {
		t.Errorf("rules not swapped")
	}
}

// TestReloadLoaderError verifies a loader error preserves the prior list.
func TestReloadLoaderError(t *testing.T) {
	_, rules, _ := LoadPersonaConfigYAML([]byte(rsSelfConfigYAML))
	engine := NewEngine(rules, NewMemoryCooldownGate())
	engine.SetLoader(func(_ context.Context) ([]Rule, error) {
		return nil, errors.New("db down")
	})
	if err := engine.Reload(context.Background()); err == nil {
		t.Errorf("expected loader error")
	}
	if len(engine.Snapshot()) != 2 {
		t.Errorf("rules should not be replaced on error: got %d", len(engine.Snapshot()))
	}
}

// --- Spec parsing & error handling ----------------------------------------

func TestUnknownPredicateFails(t *testing.T) {
	bad := []byte(`
persona: x
rules:
  - name: bad
    when:
      window.does_not_exist: 3
    fire:
      action_template: t
      destination: noop
      cooldown_seconds: 0
`)
	if _, _, err := LoadPersonaConfigYAML(bad); err == nil {
		t.Errorf("expected unknown predicate error")
	}
}

func TestMalformedRegexFails(t *testing.T) {
	bad := []byte(`
persona: x
rules:
  - name: bad-regex
    when:
      window.event_path_matches: "[unterminated"
    fire:
      action_template: t
      destination: noop
      cooldown_seconds: 0
`)
	if _, _, err := LoadPersonaConfigYAML(bad); err == nil {
		t.Errorf("expected regex compile error at load time")
	}
}

func TestMixedLogicalKeyFails(t *testing.T) {
	bad := []byte(`
persona: x
rules:
  - name: mixed
    when:
      all:
        - window.has_event_type: page
      window.event_count: { ">=": 1 }
    fire:
      action_template: t
      destination: noop
      cooldown_seconds: 0
`)
	if _, _, err := LoadPersonaConfigYAML(bad); err == nil {
		t.Errorf("expected mixed-key error")
	}
}

// --- Operator coercion -----------------------------------------------------

func TestApplyOpNumericCoercion(t *testing.T) {
	cases := []struct {
		op     string
		l, r   any
		expect bool
	}{
		{">=", 3, 3.0, true},
		{">=", 3, 4, false},
		{"==", int64(3), 3.0, true},
		{"!=", "foo", "bar", true},
		{"<", float32(2.5), 3, true},
		{"<=", 1, 1, true},
		{"in", "a", []any{"a", "b"}, true},
		{"in", "c", []any{"a", "b"}, false},
		{"in", "a", []string{"a", "b"}, true},
		{"matches", "/listings/L1", "^/listings(/|$).*", true},
		{"matches", "/about", "^/listings(/|$).*", false},
		{"unknown_op", 1, 1, false},
	}
	for i, c := range cases {
		if got := applyOp(c.op, c.l, c.r); got != c.expect {
			t.Errorf("case %d (%s, %v, %v) = %v, want %v", i, c.op, c.l, c.r, got, c.expect)
		}
	}
}

func TestRegexCacheReturnsSameInstance(t *testing.T) {
	r1, err := compileRegex("^/foo$")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	r2, err := compileRegex("^/foo$")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if r1 != r2 {
		t.Errorf("regex cache miss: expected same *regexp.Regexp")
	}
}

// --- MarshalSpec round-trip ------------------------------------------------

func TestMarshalUnmarshalSpec(t *testing.T) {
	_, rules, err := LoadPersonaConfigYAML([]byte(rsSelfConfigYAML))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, r := range rules {
		spec, err := MarshalSpec(r.When)
		if err != nil {
			t.Fatalf("marshal %s: %v", r.Name, err)
		}
		expr, err := UnmarshalSpec(spec)
		if err != nil {
			t.Fatalf("unmarshal %s: %v", r.Name, err)
		}
		if expr == nil {
			t.Errorf("nil expr after round-trip")
		}
	}
}

// --- Predicate registry coverage ------------------------------------------

func TestAllPredicatesRegistered(t *testing.T) {
	want := []string{
		"window.event_count",
		"window.event_count_of_type",
		"window.event_count_of_name",
		"window.idle_seconds",
		"window.has_event_type",
		"window.has_event_name",
		"window.event_path_matches",
		"window.has_property",
		"window.property_value",
		"window.distinct_paths_at_least",
		"window.has_error_event",
		"window.session_event_count",
		"traits.known",
		"traits.value",
	}
	got := RegisteredPredicates()
	gotSet := map[string]bool{}
	for _, n := range got {
		gotSet[n] = true
	}
	for _, n := range want {
		if !gotSet[n] {
			t.Errorf("predicate %q not registered", n)
		}
	}
}

// TestNotPredicate exercises the Not branch — not exercised by §5 configs.
func TestNotPredicate(t *testing.T) {
	spec := map[string]any{
		"not": map[string]any{
			"window.has_event_type": "page",
		},
	}
	expr, err := UnmarshalSpec(spec)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	store := window.New(0)
	t0 := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	store.Update(makeEvent(t, "track", "x", "anon", "", nil, t0))
	snap, _ := store.Snapshot("anon")
	if !expr.Eval(snap, t0) {
		t.Errorf("Not(has_event_type=page) should be true when no page events seen")
	}
	// Now add a page event.
	store.Update(makeEvent(t, "page", "", "anon", "/p", nil, t0))
	snap2, _ := store.Snapshot("anon")
	if expr.Eval(snap2, t0) {
		t.Errorf("Not(has_event_type=page) should be false when page event present")
	}
}

// TestTraitsValueAndKnown exercises traits predicates.
func TestTraitsValueAndKnown(t *testing.T) {
	spec := map[string]any{
		"all": []any{
			map[string]any{"traits.known": "plan"},
			map[string]any{"traits.value": map[string]any{"path": "plan", "==": "free"}},
		},
	}
	expr, err := UnmarshalSpec(spec)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	store := window.New(0)
	t0 := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	store.Update(&event.Event{
		Type:              "identify",
		AnonymousID:       "u1",
		MessageID:         "m",
		OriginalTimestamp: t0,
		Traits:            json.RawMessage(`{"plan":"free"}`),
	})
	snap, _ := store.Snapshot("u1")
	if !expr.Eval(snap, t0) {
		t.Errorf("expected traits.value to match")
	}
}

// TestDistinctPathsAtLeast exercises the distinct paths predicate.
func TestDistinctPathsAtLeast(t *testing.T) {
	spec := map[string]any{"window.distinct_paths_at_least": 2}
	expr, err := UnmarshalSpec(spec)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	store := window.New(0)
	t0 := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	store.Update(makeEvent(t, "page", "", "u", "/a", nil, t0))
	snap, _ := store.Snapshot("u")
	if expr.Eval(snap, t0) {
		t.Errorf("1 distinct path should fail >=2")
	}
	store.Update(makeEvent(t, "page", "", "u", "/b", nil, t0))
	snap2, _ := store.Snapshot("u")
	if !expr.Eval(snap2, t0) {
		t.Errorf("2 distinct paths should satisfy >=2")
	}
}

// --- Counting gate helper --------------------------------------------------

type countingGate struct {
	inner      CooldownGate
	allowCalls int32
	markCalls  int32
}

func (g *countingGate) Allow(anonID, rule string, now time.Time) bool {
	atomic.AddInt32(&g.allowCalls, 1)
	return g.inner.Allow(anonID, rule, now)
}

func (g *countingGate) Mark(anonID, rule string, now time.Time, cd time.Duration) {
	atomic.AddInt32(&g.markCalls, 1)
	g.inner.Mark(anonID, rule, now, cd)
}
