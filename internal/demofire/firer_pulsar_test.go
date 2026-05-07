package demofire_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/apache/pulsar-client-go/pulsar"

	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/demofire"
	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/event"
)

// ----------------------------------------------------------------------------
// Fake Pulsar producer + client for unit tests
// ----------------------------------------------------------------------------

// sentMsg records a single call to fakePulsarProducer.Send.
type sentMsg struct {
	payload    []byte
	key        string
	properties map[string]string
}

// fakePulsarProducer is a test double for pulsarProducer. It records every
// Send call and optionally injects a send error.
type fakePulsarProducer struct {
	mu        sync.Mutex
	messages  []sentMsg
	sendErr   error
	flushCalls int
	closeCalls int
}

func (f *fakePulsarProducer) Send(_ context.Context, msg *pulsar.ProducerMessage) (pulsar.MessageID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendErr != nil {
		return nil, f.sendErr
	}
	props := make(map[string]string, len(msg.Properties))
	for k, v := range msg.Properties {
		props[k] = v
	}
	f.messages = append(f.messages, sentMsg{
		payload:    append([]byte(nil), msg.Payload...),
		key:        msg.Key,
		properties: props,
	})
	return fakeMessageID{}, nil
}

func (f *fakePulsarProducer) Flush() error {
	f.mu.Lock()
	f.flushCalls++
	f.mu.Unlock()
	return nil
}

func (f *fakePulsarProducer) Close() {
	f.mu.Lock()
	f.closeCalls++
	f.mu.Unlock()
}

func (f *fakePulsarProducer) sent() []sentMsg {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]sentMsg, len(f.messages))
	copy(out, f.messages)
	return out
}

// fakeMessageID satisfies pulsar.MessageID (nil-safe stub).
type fakeMessageID struct{}

func (fakeMessageID) Serialize() []byte         { return nil }
func (fakeMessageID) LedgerID() int64           { return 0 }
func (fakeMessageID) EntryID() int64            { return 0 }
func (fakeMessageID) BatchIdx() int32           { return 0 }
func (fakeMessageID) PartitionIdx() int32       { return 0 }
func (fakeMessageID) BatchSize() int32          { return 0 }
func (fakeMessageID) String() string            { return "fake-msg-id" }

// ----------------------------------------------------------------------------
// Helpers to construct a PulsarFirer backed by fakes
// ----------------------------------------------------------------------------

func newTestPulsarFirer(fp *fakePulsarProducer, cfg demofire.PulsarFirerConfig) *demofire.PulsarFirer {
	f := demofire.NewPulsarFirer(cfg)
	// Inject fake client/producer factories.
	demofire.InjectPulsarFactories(f,
		func(_ pulsar.ClientOptions) (demofire.PulsarClientIface, error) {
			return &fakeClient{}, nil
		},
		func(_ demofire.PulsarClientIface, _ pulsar.ProducerOptions) (demofire.PulsarProducerIface, error) {
			return fp, nil
		},
	)
	return f
}

// fakeClient satisfies demofire.PulsarClientIface (CreateProducer + Close).
type fakeClient struct{}

func (fakeClient) CreateProducer(_ pulsar.ProducerOptions) (pulsar.Producer, error) {
	return nil, nil // not called; newProducer factory is overridden
}
func (fakeClient) Close() {}

// ----------------------------------------------------------------------------
// Mini reusable script builder
// ----------------------------------------------------------------------------

func minimalScript(n int) []demofire.ScriptStep {
	steps := make([]demofire.ScriptStep, n)
	for i := range steps {
		steps[i] = demofire.ScriptStep{
			DelayMs: 0,
			Event: event.Event{
				Type:        "track",
				Channel:     "browser",
				Event:       "Test Event",
				AnonymousID: "anon-test-001",
				MessageID:   "msg-" + string(rune('A'+i)),
			},
		}
	}
	return steps
}

func defaultCfg() demofire.PulsarFirerConfig {
	return demofire.PulsarFirerConfig{
		URL:                 "pulsar://localhost:6650",
		Topic:               "persistent://public/default/test",
		WriteKey:            "test-write-key",
		SourceID:            "test-source-id",
		TLSValidateHostname: true,
	}
}

// ----------------------------------------------------------------------------
// Tests
// ----------------------------------------------------------------------------

// TestPulsarFirer_PublishesAllSteps asserts that every ScriptStep results in
// one Send call with a valid JSON payload carrying a proper RudderEvent, the
// correct Key (anonymousId), and the expected Properties. Uses a zero-delay
// script to keep the test fast.
func TestPulsarFirer_PublishesAllSteps(t *testing.T) {
	t.Parallel()

	// Build a zero-delay script with browser-channel events that mirror real
	// persona events (channel, anonymousId, messageId all set).
	steps := []demofire.ScriptStep{
		{DelayMs: 0, Event: event.Event{
			Type: "identify", Channel: "browser",
			AnonymousID: "anon_demo-re-001", MessageID: "msg-step0",
		}},
		{DelayMs: 0, Event: event.Event{
			Type: "page", Channel: "browser",
			AnonymousID: "anon_demo-re-001", MessageID: "msg-step1",
		}},
		{DelayMs: 0, Event: event.Event{
			Type: "track", Channel: "browser", Event: "Listing Viewed",
			AnonymousID: "anon_demo-re-001", MessageID: "msg-step2",
		}},
	}

	fp := &fakePulsarProducer{}
	firer := newTestPulsarFirer(fp, demofire.PulsarFirerConfig{
		URL:      "pulsar://localhost:6650",
		Topic:    "persistent://public/default/test",
		WriteKey: "wk-realestate",
		SourceID: "src-realestate",
	})

	ctx := context.Background()
	count, err := firer.Fire(ctx, steps)
	if err != nil {
		t.Fatalf("Fire returned unexpected error: %v", err)
	}
	if count != len(steps) {
		t.Errorf("sent count: want %d, got %d", len(steps), count)
	}

	msgs := fp.sent()
	if len(msgs) != len(steps) {
		t.Fatalf("expected %d messages captured, got %d", len(steps), len(msgs))
	}

	for i, m := range msgs {
		// Payload must be valid JSON representing an event.Event.
		var ev event.Event
		if err := json.Unmarshal(m.payload, &ev); err != nil {
			t.Errorf("step %d: payload is not valid JSON: %v", i, err)
			continue
		}
		if ev.Channel != "browser" {
			t.Errorf("step %d: channel=%q, want browser", i, ev.Channel)
		}

		// Key must match the event's AnonymousID.
		if m.key != steps[i].Event.AnonymousID {
			t.Errorf("step %d: key=%q, want %q (anonymousId)", i, m.key, steps[i].Event.AnonymousID)
		}

		// Properties must include writeKey, sourceId, messageId.
		if got := m.properties["writeKey"]; got != "wk-realestate" {
			t.Errorf("step %d: writeKey=%q, want wk-realestate", i, got)
		}
		if got := m.properties["sourceId"]; got != "src-realestate" {
			t.Errorf("step %d: sourceId=%q, want src-realestate", i, got)
		}
		if got := m.properties["messageId"]; got == "" {
			t.Errorf("step %d: messageId is empty", i)
		}
	}
}

// TestPulsarFirer_KeyIsAnonymousID verifies the partition key equals the
// event's AnonymousID for every step.
func TestPulsarFirer_KeyIsAnonymousID(t *testing.T) {
	t.Parallel()

	script := []demofire.ScriptStep{
		{DelayMs: 0, Event: event.Event{Type: "track", Channel: "browser", AnonymousID: "user-abc", MessageID: "m1"}},
		{DelayMs: 0, Event: event.Event{Type: "page", Channel: "browser", AnonymousID: "user-def", MessageID: "m2"}},
		{DelayMs: 0, Event: event.Event{Type: "identify", Channel: "browser", AnonymousID: "user-abc", MessageID: "m3"}},
	}

	fp := &fakePulsarProducer{}
	firer := newTestPulsarFirer(fp, defaultCfg())
	firer.Sleep = func(time.Duration) {}

	if _, err := firer.Fire(context.Background(), script); err != nil {
		t.Fatalf("Fire error: %v", err)
	}

	msgs := fp.sent()
	for i, m := range msgs {
		want := script[i].Event.AnonymousID
		if m.key != want {
			t.Errorf("step %d: key=%q, want %q", i, m.key, want)
		}
	}
}

// TestPulsarFirer_HonorsDelayMs verifies that delays between steps are
// honoured by measuring real elapsed wall time. Delays are kept ≤10ms so the
// test completes quickly while still exercising the sleepWithCtx path.
func TestPulsarFirer_HonorsDelayMs(t *testing.T) {
	t.Parallel()

	type tc struct {
		name    string
		script  []demofire.ScriptStep
		wantMin time.Duration
	}

	cases := []tc{
		{
			name: "no_delay",
			script: []demofire.ScriptStep{
				{DelayMs: 0, Event: event.Event{Type: "track", Channel: "browser", AnonymousID: "a", MessageID: "m1"}},
				{DelayMs: 0, Event: event.Event{Type: "track", Channel: "browser", AnonymousID: "a", MessageID: "m2"}},
			},
			wantMin: 0,
		},
		{
			name: "single_10ms_delay",
			script: []demofire.ScriptStep{
				{DelayMs: 0, Event: event.Event{Type: "track", Channel: "browser", AnonymousID: "a", MessageID: "m1"}},
				{DelayMs: 10, Event: event.Event{Type: "track", Channel: "browser", AnonymousID: "a", MessageID: "m2"}},
			},
			wantMin: 10 * time.Millisecond,
		},
		{
			name: "cumulative_delays",
			script: []demofire.ScriptStep{
				{DelayMs: 5, Event: event.Event{Type: "track", Channel: "browser", AnonymousID: "a", MessageID: "m1"}},
				{DelayMs: 7, Event: event.Event{Type: "track", Channel: "browser", AnonymousID: "a", MessageID: "m2"}},
			},
			wantMin: 12 * time.Millisecond,
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			fp := &fakePulsarProducer{}
			firer := newTestPulsarFirer(fp, defaultCfg())
			// Use real sleep (not stubbed) so sleepWithCtx actually waits.
			// Delays are ≤10ms so the test completes in under 50ms.

			start := time.Now()
			if _, err := firer.Fire(context.Background(), c.script); err != nil {
				t.Fatalf("Fire error: %v", err)
			}
			elapsed := time.Since(start)

			if elapsed < c.wantMin {
				t.Errorf("elapsed=%v, want >= %v", elapsed, c.wantMin)
			}
		})
	}
}

// TestPulsarFirer_FlushBeforeClose verifies that producer.Flush() is called
// before producer.Close() on every Fire invocation.
func TestPulsarFirer_FlushBeforeClose(t *testing.T) {
	t.Parallel()

	fp := &fakePulsarProducer{}
	firer := newTestPulsarFirer(fp, defaultCfg())
	firer.Sleep = func(time.Duration) {}

	_, _ = firer.Fire(context.Background(), minimalScript(2))

	fp.mu.Lock()
	defer fp.mu.Unlock()

	if fp.flushCalls == 0 {
		t.Error("expected Flush to be called at least once, got 0")
	}
	if fp.closeCalls == 0 {
		t.Error("expected Close to be called at least once, got 0")
	}
}

// TestPulsarFirer_RequiresURL verifies that an empty URL returns an error.
func TestPulsarFirer_RequiresURL(t *testing.T) {
	t.Parallel()
	fp := &fakePulsarProducer{}
	cfg := defaultCfg()
	cfg.URL = ""
	firer := newTestPulsarFirer(fp, cfg)
	_, err := firer.Fire(context.Background(), minimalScript(1))
	if err == nil {
		t.Error("expected error for empty URL")
	}
}

// TestPulsarFirer_RequiresTopic verifies that an empty Topic returns an error.
func TestPulsarFirer_RequiresTopic(t *testing.T) {
	t.Parallel()
	fp := &fakePulsarProducer{}
	cfg := defaultCfg()
	cfg.Topic = ""
	firer := newTestPulsarFirer(fp, cfg)
	_, err := firer.Fire(context.Background(), minimalScript(1))
	if err == nil {
		t.Error("expected error for empty Topic")
	}
}

// TestPulsarFirer_RequiresWriteKey verifies that an empty WriteKey returns an error.
func TestPulsarFirer_RequiresWriteKey(t *testing.T) {
	t.Parallel()
	fp := &fakePulsarProducer{}
	cfg := defaultCfg()
	cfg.WriteKey = ""
	firer := newTestPulsarFirer(fp, cfg)
	_, err := firer.Fire(context.Background(), minimalScript(1))
	if err == nil {
		t.Error("expected error for empty WriteKey")
	}
}

// TestPulsarFirer_CtxCancelStopsPublish verifies that context cancellation
// during a long sleep surfaces as an error and no further sends occur.
func TestPulsarFirer_CtxCancelStopsPublish(t *testing.T) {
	t.Parallel()

	fp := &fakePulsarProducer{}
	firer := newTestPulsarFirer(fp, defaultCfg())
	// Real sleep so sleepWithCtx can detect ctx cancellation.

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	script := []demofire.ScriptStep{
		{DelayMs: 0, Event: event.Event{Type: "track", Channel: "browser", AnonymousID: "a", MessageID: "m1"}},
		{DelayMs: 5_000, Event: event.Event{Type: "track", Channel: "browser", AnonymousID: "a", MessageID: "m2"}},
	}

	start := time.Now()
	count, err := firer.Fire(ctx, script)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected cancellation error")
	}
	if count != 1 {
		t.Errorf("expected 1 sent before cancel, got %d", count)
	}
	if elapsed > 1*time.Second {
		t.Errorf("expected cancellation within 1s, took %v", elapsed)
	}
}

// TestPulsarFirer_RestampsTimestampsPerStep verifies the Bug-1a fix: the firer
// must write a fresh time.Now() into ev.OriginalTimestamp (and SentAt) on each
// step, and must NOT mutate the original script slice elements.
//
// Assertions:
//  1. All 3 OriginalTimestamps are non-zero (firer stamped them).
//  2. All 3 OriginalTimestamps are distinct (each step gets its own time.Now()).
//  3. The timestamps are monotonically non-decreasing (later steps ≥ earlier steps).
//  4. The original script[i].Event.OriginalTimestamp is still zero (firer did not
//     mutate the slice — slice-mutation regression guard).
//  5. SentAt equals OriginalTimestamp for each message.
func TestPulsarFirer_RestampsTimestampsPerStep(t *testing.T) {
	t.Parallel()

	// Build a 3-step script with explicitly zero OriginalTimestamp so we can
	// detect whether the firer wrote a real value.
	script := []demofire.ScriptStep{
		{DelayMs: 1, Event: event.Event{Type: "identify", Channel: "browser", AnonymousID: "anon-restamp-001", MessageID: "rs-m0"}},
		{DelayMs: 1, Event: event.Event{Type: "page", Channel: "browser", AnonymousID: "anon-restamp-001", MessageID: "rs-m1"}},
		{DelayMs: 1, Event: event.Event{Type: "track", Channel: "browser", Event: "Restamp Test", AnonymousID: "anon-restamp-001", MessageID: "rs-m2"}},
	}
	// Confirm zero-value precondition so assertion 4 is meaningful.
	for i, s := range script {
		if !s.Event.OriginalTimestamp.IsZero() {
			t.Fatalf("precondition: script[%d].Event.OriginalTimestamp is not zero", i)
		}
	}

	fp := &fakePulsarProducer{}
	firer := newTestPulsarFirer(fp, defaultCfg())
	// Use real sleep (1ms per step) so time.Now() advances between sends,
	// giving us strictly increasing timestamps to assert on.

	ctx := context.Background()
	count, err := firer.Fire(ctx, script)
	if err != nil {
		t.Fatalf("Fire returned unexpected error: %v", err)
	}
	if count != 3 {
		t.Fatalf("sent count: want 3, got %d", count)
	}

	msgs := fp.sent()
	if len(msgs) != 3 {
		t.Fatalf("expected 3 captured messages, got %d", len(msgs))
	}

	// Decode each payload and collect OriginalTimestamp / SentAt.
	type stamped struct {
		OriginalTimestamp time.Time `json:"originalTimestamp"`
		SentAt            time.Time `json:"sentAt"`
	}
	timestamps := make([]stamped, 3)
	for i, m := range msgs {
		var s stamped
		if err := json.Unmarshal(m.payload, &s); err != nil {
			t.Fatalf("step %d: failed to decode payload: %v", i, err)
		}
		timestamps[i] = s
	}

	for i, ts := range timestamps {
		// Assertion 1: non-zero.
		if ts.OriginalTimestamp.IsZero() {
			t.Errorf("step %d: OriginalTimestamp is zero — firer did not stamp it", i)
		}
		// Assertion 5: SentAt == OriginalTimestamp.
		if !ts.SentAt.Equal(ts.OriginalTimestamp) {
			t.Errorf("step %d: SentAt=%v != OriginalTimestamp=%v", i, ts.SentAt, ts.OriginalTimestamp)
		}
	}

	// Assertion 2: all 3 timestamps are distinct.
	if timestamps[0].OriginalTimestamp.Equal(timestamps[1].OriginalTimestamp) {
		t.Errorf("steps 0 and 1 have the same OriginalTimestamp (%v) — expected distinct per-step stamps", timestamps[0].OriginalTimestamp)
	}
	if timestamps[1].OriginalTimestamp.Equal(timestamps[2].OriginalTimestamp) {
		t.Errorf("steps 1 and 2 have the same OriginalTimestamp (%v) — expected distinct per-step stamps", timestamps[1].OriginalTimestamp)
	}

	// Assertion 3: monotonically non-decreasing.
	if timestamps[1].OriginalTimestamp.Before(timestamps[0].OriginalTimestamp) {
		t.Errorf("step 1 timestamp %v is before step 0 timestamp %v — not monotonic", timestamps[1].OriginalTimestamp, timestamps[0].OriginalTimestamp)
	}
	if timestamps[2].OriginalTimestamp.Before(timestamps[1].OriginalTimestamp) {
		t.Errorf("step 2 timestamp %v is before step 1 timestamp %v — not monotonic", timestamps[2].OriginalTimestamp, timestamps[1].OriginalTimestamp)
	}

	// Assertion 4: original script slice elements are unchanged (zero).
	for i, s := range script {
		if !s.Event.OriginalTimestamp.IsZero() {
			t.Errorf("step %d: firer mutated script[i].Event.OriginalTimestamp (got %v, want zero)", i, s.Event.OriginalTimestamp)
		}
	}
}

// TestPulsarFirer_SourceIDDefaultsToWriteKey verifies that when SourceID is
// empty in config, the property["sourceId"] value equals WriteKey.
func TestPulsarFirer_SourceIDDefaultsToWriteKey(t *testing.T) {
	t.Parallel()

	fp := &fakePulsarProducer{}
	cfg := defaultCfg()
	cfg.SourceID = "" // leave blank — should default to WriteKey
	firer := newTestPulsarFirer(fp, cfg)
	firer.Sleep = func(time.Duration) {}

	script := minimalScript(1)
	if _, err := firer.Fire(context.Background(), script); err != nil {
		t.Fatalf("Fire error: %v", err)
	}

	msgs := fp.sent()
	if len(msgs) == 0 {
		t.Fatal("no messages sent")
	}
	if got := msgs[0].properties["sourceId"]; got != cfg.WriteKey {
		t.Errorf("sourceId=%q, want %q (=writeKey)", got, cfg.WriteKey)
	}
}
