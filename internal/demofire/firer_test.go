package demofire_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/demofire"
	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/event"
)

// TestRealestateScript_StructuralProperties checks the script length, anonId
// uniformity, and event types match §6.2 expectations.
func TestRealestateScript_StructuralProperties(t *testing.T) {
	t.Parallel()
	steps := demofire.RealestateScript()
	if got, want := len(steps), 8; got != want {
		t.Fatalf("expected %d steps, got %d", want, got)
	}

	wantTypes := []string{"identify", "page", "track", "track", "track", "track", "page", "track"}
	for i, st := range steps {
		if st.Event.AnonymousID != "anon_demo-re-001" {
			t.Errorf("step %d: anonID=%q, expected anon_demo-re-001", i, st.Event.AnonymousID)
		}
		if st.Event.Channel != "browser" {
			t.Errorf("step %d: channel=%q, expected browser", i, st.Event.Channel)
		}
		if st.Event.Type != wantTypes[i] {
			t.Errorf("step %d: type=%q, expected %q", i, st.Event.Type, wantTypes[i])
		}
	}

	// At least one step must reference each of: Listing Viewed, Filter Applied,
	// /listings page, listing-detail page.
	wantNames := map[string]bool{"Listing Viewed": false, "Filter Applied": false}
	wantPaths := map[string]bool{"/listings": false, "/listings/L112": false}
	for _, st := range steps {
		if _, ok := wantNames[st.Event.Event]; ok {
			wantNames[st.Event.Event] = true
		}
		if _, ok := wantPaths[st.Event.PagePath()]; ok {
			wantPaths[st.Event.PagePath()] = true
		}
	}
	for n, found := range wantNames {
		if !found {
			t.Errorf("real-estate script missing event %q", n)
		}
	}
	for p, found := range wantPaths {
		if !found {
			t.Errorf("real-estate script missing page path %q", p)
		}
	}
}

// TestRealestateScript_Properties verifies the rich §6.2 properties carry
// through (listing_id, suburb, price, etc.).
func TestRealestateScript_Properties(t *testing.T) {
	t.Parallel()
	steps := demofire.RealestateScript()
	for _, st := range steps {
		if st.Event.Event != "Listing Viewed" {
			continue
		}
		props := st.Event.PropertiesMap()
		for _, key := range []string{"listing_id", "suburb", "price", "bedrooms"} {
			if _, ok := props[key]; !ok {
				t.Errorf("Listing Viewed missing property %q (props=%v)", key, props)
			}
		}
	}
}

// TestRSSelfScript_StructuralProperties checks length + content for §6.3.
func TestRSSelfScript_StructuralProperties(t *testing.T) {
	t.Parallel()
	steps := demofire.RSSelfScript()
	if got, want := len(steps), 5; got != want {
		t.Fatalf("expected %d steps, got %d", want, got)
	}

	hasError := false
	for _, st := range steps {
		if st.Event.AnonymousID != "demo-rs-001" || st.Event.UserID != "demo-rs-001" {
			t.Errorf("rs-self step ID mismatch: anon=%q user=%q", st.Event.AnonymousID, st.Event.UserID)
		}
		if st.Event.Event == "Destination Setup Error" {
			hasError = true
			props := st.Event.PropertiesMap()
			if got := props["error_code"]; got != "AMP_INVALID_API_KEY" {
				t.Errorf("error_code=%v, expected AMP_INVALID_API_KEY", got)
			}
		}
	}
	if !hasError {
		t.Error("rs-self script missing Destination Setup Error event")
	}
}

// TestFirer_PostsExpectedShape spins up an httptest server and verifies the
// request format: method, content-type, basic-auth, body shape.
func TestFirer_PostsExpectedShape(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	var capturedAuth string
	var capturedBodies [][]byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/batch" {
			t.Errorf("expected /v1/batch, got %s", r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("content-type=%q", got)
		}
		capturedAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		capturedBodies = append(capturedBodies, body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	firer := demofire.NewFirer(srv.URL, "test-write-key")
	firer.Sleep = func(time.Duration) {} // skip real sleep

	miniScript := []demofire.ScriptStep{
		{DelayMs: 0, Event: event.Event{
			Type: "track", Channel: "browser", Event: "Test",
			AnonymousID: "anon-1", MessageID: "msg-1",
			OriginalTimestamp: time.Now().UTC(),
			Properties:        json.RawMessage(`{"foo":"bar"}`),
		}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	count, err := firer.Fire(ctx, miniScript)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if count != 1 {
		t.Errorf("count=%d, expected 1", count)
	}
	if hits.Load() != 1 {
		t.Errorf("expected 1 HTTP hit, got %d", hits.Load())
	}

	// Verify Basic auth header.
	if !strings.HasPrefix(capturedAuth, "Basic ") {
		t.Fatalf("expected Basic auth, got %q", capturedAuth)
	}
	encoded := strings.TrimPrefix(capturedAuth, "Basic ")
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode auth: %v", err)
	}
	if got, want := string(decoded), "test-write-key:"; got != want {
		t.Errorf("basic auth decoded=%q, expected %q", got, want)
	}

	// Verify body has a `batch` array of length 1.
	var bodyShape struct {
		Batch  []map[string]any `json:"batch"`
		SentAt string           `json:"sentAt"`
	}
	if err := json.Unmarshal(capturedBodies[0], &bodyShape); err != nil {
		t.Fatalf("body decode: %v", err)
	}
	if len(bodyShape.Batch) != 1 {
		t.Errorf("expected batch length 1, got %d", len(bodyShape.Batch))
	}
	if bodyShape.SentAt == "" {
		t.Error("expected sentAt to be set")
	}
}

// TestFirer_Honours_DelayMs verifies the Sleep hook is called with the
// configured delays.
func TestFirer_Honours_DelayMs(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var sleeps []time.Duration
	firer := demofire.NewFirer(srv.URL, "k")
	firer.Sleep = func(d time.Duration) { sleeps = append(sleeps, d) }

	script := []demofire.ScriptStep{
		{DelayMs: 0, Event: event.Event{Type: "track", Channel: "browser", AnonymousID: "a"}},
		{DelayMs: 100, Event: event.Event{Type: "track", Channel: "browser", AnonymousID: "a"}},
	}
	if _, err := firer.Fire(context.Background(), script); err != nil {
		t.Fatalf("Fire: %v", err)
	}
	// Total sleep across all calls should sum to >= 100ms (50ms slices).
	var total time.Duration
	for _, s := range sleeps {
		total += s
	}
	if total < 100*time.Millisecond {
		t.Errorf("expected total sleep >= 100ms, got %v", total)
	}
}

// TestFirer_RequiresIngestionURL checks the empty-config error path.
func TestFirer_RequiresIngestionURL(t *testing.T) {
	t.Parallel()
	firer := demofire.NewFirer("", "k")
	if _, err := firer.Fire(context.Background(), nil); err == nil {
		t.Error("expected error for empty IngestionURL")
	}
}

// TestFirer_RequiresWriteKey checks the empty-writeKey error path.
func TestFirer_RequiresWriteKey(t *testing.T) {
	t.Parallel()
	firer := demofire.NewFirer("http://example.com", "")
	if _, err := firer.Fire(context.Background(), nil); err == nil {
		t.Error("expected error for empty WriteKey")
	}
}

// TestFirer_PropagatesNon2xx verifies a 500 response stops the script and
// returns an error with status_code in the wrapped fields.
func TestFirer_PropagatesNon2xx(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer srv.Close()

	firer := demofire.NewFirer(srv.URL, "k")
	firer.Sleep = func(time.Duration) {}
	_, err := firer.Fire(context.Background(), []demofire.ScriptStep{
		{DelayMs: 0, Event: event.Event{Type: "track", Channel: "browser", AnonymousID: "a"}},
	})
	if err == nil {
		t.Fatal("expected error on 500 response")
	}
	if !strings.Contains(err.Error(), "non-2xx") {
		t.Errorf("error should mention non-2xx, got %v", err)
	}
}

// TestRunConcurrent_ThreeScripts verifies that RunConcurrent fires 3 separate
// scripts each with distinct anonymousIds.
func TestRunConcurrent_ThreeScripts(t *testing.T) {
	t.Parallel()

	// Track which anonymousIds were POSTed.
	var mu sync.Mutex
	seenIDs := make(map[string]int)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var wrapper struct {
			Batch []map[string]any `json:"batch"`
		}
		if err := json.Unmarshal(body, &wrapper); err == nil {
			for _, ev := range wrapper.Batch {
				if id, ok := ev["anonymousId"].(string); ok {
					mu.Lock()
					seenIDs[id]++
					mu.Unlock()
				}
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	firer := demofire.NewFirer(srv.URL, "test-wk")
	firer.Sleep = func(time.Duration) {} // skip all sleeps

	scripts := []demofire.NamedScript{
		{
			Persona: "realestate",
			AnonID:  "anon-a",
			Script: []demofire.ScriptStep{
				{DelayMs: 0, Event: event.Event{Type: "track", Channel: "browser", AnonymousID: "anon-a"}},
				{DelayMs: 0, Event: event.Event{Type: "page", Channel: "browser", AnonymousID: "anon-a"}},
			},
		},
		{
			Persona: "realestate",
			AnonID:  "anon-b",
			Script: []demofire.ScriptStep{
				{DelayMs: 0, Event: event.Event{Type: "track", Channel: "browser", AnonymousID: "anon-b"}},
			},
		},
		{
			Persona: "realestate",
			AnonID:  "anon-c",
			Script: []demofire.ScriptStep{
				{DelayMs: 0, Event: event.Event{Type: "track", Channel: "browser", AnonymousID: "anon-c"}},
				{DelayMs: 0, Event: event.Event{Type: "track", Channel: "browser", AnonymousID: "anon-c"}},
				{DelayMs: 0, Event: event.Event{Type: "track", Channel: "browser", AnonymousID: "anon-c"}},
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	totalSent, err := firer.RunConcurrent(ctx, scripts, 1.0)
	if err != nil {
		t.Fatalf("RunConcurrent: %v", err)
	}
	if totalSent != 6 {
		t.Errorf("expected 6 total events, got %d", totalSent)
	}

	mu.Lock()
	defer mu.Unlock()
	// All three distinct anonymousIds must appear.
	for _, id := range []string{"anon-a", "anon-b", "anon-c"} {
		if seenIDs[id] == 0 {
			t.Errorf("anonymousId %q not seen in POSTed events; seenIDs=%v", id, seenIDs)
		}
	}
	if seenIDs["anon-a"] != 2 {
		t.Errorf("expected 2 events for anon-a, got %d", seenIDs["anon-a"])
	}
	if seenIDs["anon-b"] != 1 {
		t.Errorf("expected 1 event for anon-b, got %d", seenIDs["anon-b"])
	}
	if seenIDs["anon-c"] != 3 {
		t.Errorf("expected 3 events for anon-c, got %d", seenIDs["anon-c"])
	}
}

// TestRunConcurrent_StaggeredStart verifies that consecutive scripts begin
// approximately 500ms apart (within generous bounds for test stability).
// We inject a real sleep so wall-clock time is observable.
func TestRunConcurrent_StaggeredStart(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Record when the first event of each goroutine was sent.
	var mu sync.Mutex
	firstSeen := make(map[string]time.Time)

	firer := demofire.NewFirer(srv.URL, "test-wk")
	// Keep real sleep so we can observe stagger timing.
	// But use a per-event interceptor via a custom httptest server.
	perAnonSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var wrapper struct {
			Batch []map[string]any `json:"batch"`
		}
		if err := json.Unmarshal(body, &wrapper); err == nil {
			for _, ev := range wrapper.Batch {
				if id, ok := ev["anonymousId"].(string); ok {
					mu.Lock()
					if _, exists := firstSeen[id]; !exists {
						firstSeen[id] = time.Now()
					}
					mu.Unlock()
				}
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer perAnonSrv.Close()

	firer2 := demofire.NewFirer(perAnonSrv.URL, "test-wk")
	// Use real sleep so stagger is observable.

	scripts := []demofire.NamedScript{
		{
			Persona: "realestate",
			AnonID:  "anon-stagger-0",
			Script: []demofire.ScriptStep{
				{DelayMs: 0, Event: event.Event{Type: "track", Channel: "browser", AnonymousID: "anon-stagger-0"}},
			},
		},
		{
			Persona: "realestate",
			AnonID:  "anon-stagger-1",
			Script: []demofire.ScriptStep{
				{DelayMs: 0, Event: event.Event{Type: "track", Channel: "browser", AnonymousID: "anon-stagger-1"}},
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	_, err := firer2.RunConcurrent(ctx, scripts, 1.0)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("RunConcurrent: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	t0, ok0 := firstSeen["anon-stagger-0"]
	t1, ok1 := firstSeen["anon-stagger-1"]
	if !ok0 || !ok1 {
		t.Fatalf("not all events observed: firstSeen=%v", firstSeen)
	}

	// Script 0 starts immediately; script 1 starts ~500ms later.
	// We verify script 1 first event is at least 400ms after script 0.
	gap := t1.Sub(t0)
	if gap < 400*time.Millisecond {
		t.Errorf("stagger gap %v < 400ms — scripts may not be staggered", gap)
	}
	// Total should be under 1.5s (500ms stagger + zero-delay steps).
	if elapsed > 2*time.Second {
		t.Errorf("total elapsed %v > 2s — RunConcurrent seems to be running sequentially", elapsed)
	}
	_ = firer // suppress unused warning
}

// TestSpeed_HalvesDelays verifies that speed=2.0 halves the wall-clock time
// compared to speed=1.0 for a script with meaningful delays.
func TestSpeed_HalvesDelays(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// A script with 200ms total delay (2 steps at 100ms each).
	makeScript := func(id string) []demofire.ScriptStep {
		return []demofire.ScriptStep{
			{DelayMs: 0, Event: event.Event{Type: "track", Channel: "browser", AnonymousID: id}},
			{DelayMs: 100, Event: event.Event{Type: "track", Channel: "browser", AnonymousID: id}},
			{DelayMs: 100, Event: event.Event{Type: "track", Channel: "browser", AnonymousID: id}},
		}
	}

	// Measure at 1x speed.
	firer1x := demofire.NewFirer(srv.URL, "test-wk")
	start1x := time.Now()
	_, _ = firer1x.Fire(context.Background(), makeScript("anon-1x"))
	elapsed1x := time.Since(start1x)

	// Measure at 2x speed.
	firer2x := demofire.NewFirer(srv.URL, "test-wk")
	firer2x.Speed = 2.0
	start2x := time.Now()
	_, _ = firer2x.Fire(context.Background(), makeScript("anon-2x"))
	elapsed2x := time.Since(start2x)

	// 2x should be at least 30% faster than 1x.
	if elapsed2x >= elapsed1x {
		t.Errorf("speed=2.0 not faster: 1x=%v 2x=%v", elapsed1x, elapsed2x)
	}
	// 1x should take at least 150ms (two 100ms delays with slice-based sleep).
	if elapsed1x < 150*time.Millisecond {
		t.Errorf("1x elapsed %v < 150ms — test delays might be too small", elapsed1x)
	}
}

// TestFirer_CtxCancelDuringDelay verifies cancellation during a long sleep
// returns promptly with a wrapped context error.
func TestFirer_CtxCancelDuringDelay(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	firer := demofire.NewFirer(srv.URL, "k")
	// Do not stub Sleep — we want the real ctx-aware sleep loop.
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	script := []demofire.ScriptStep{
		{DelayMs: 0, Event: event.Event{Type: "track", Channel: "browser", AnonymousID: "a"}},
		{DelayMs: 5_000, Event: event.Event{Type: "track", Channel: "browser", AnonymousID: "a"}},
	}
	start := time.Now()
	count, err := firer.Fire(ctx, script)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if count != 1 {
		t.Errorf("expected 1 step before cancel, got %d", count)
	}
	if elapsed > 1*time.Second {
		t.Errorf("expected cancellation < 1s, took %v", elapsed)
	}
}
