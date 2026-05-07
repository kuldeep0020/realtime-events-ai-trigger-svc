package demofire_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/demofire"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/event"
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
