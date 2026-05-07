package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/sse"
)

// newTestServer wires a Server with a fresh hub and an in-memory SeedFS.
// pool is intentionally nil so DB-backed endpoints return 503 — those are
// covered by integration tests in WP-A.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	seed := NewMapSeedFS(map[string][]byte{
		"persona-configs/realestate.yaml": []byte("persona: realestate\nrules: []\n"),
		"persona-configs/rs-self.yaml":    []byte("persona: rs-self\nrules: []\n"),
	})
	srv := New(Config{
		Pool: nil,
		Hub:  sse.NewHub(sse.WithHeartbeatInterval(20 * time.Millisecond)),
		Seed: seed,
	})
	t.Cleanup(func() {
		_ = srv.hub.Close(context.Background())
	})
	return srv
}

func TestHealthz_AlwaysOK(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Errorf("expected ok status in body, got %s", rec.Body.String())
	}
}

func TestReadyz_NilPool_StillOK(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestMetrics_Render(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	// Hit healthz once to populate counters.
	srv.Handler().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics: expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "http_requests_total") {
		t.Errorf("expected http_requests_total in metrics body, got: %s", body)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Errorf("expected text/plain content-type, got %q", got)
	}
}

func TestTrackingPlan_RequiresPersona(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	// chi requires the {persona} URL param to be present; absent it the route
	// won't match and we get 404.
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tracking-plan/", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing persona, got %d", rec.Code)
	}
}

func TestTrackingPlan_NoDB_503(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tracking-plan/realestate", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when pool is nil, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"error"`) {
		t.Errorf("expected {error: ...} body, got %s", rec.Body.String())
	}
}

func TestGenerateConfig_Realestate(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	body, _ := json.Marshal(map[string]any{
		"persona": "realestate",
		"answers": map[string]any{"region": "IN"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/generate-config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp generateConfigResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	if resp.Persona != "realestate" {
		t.Errorf("expected persona=realestate, got %q", resp.Persona)
	}
	if !strings.Contains(resp.ConfigYAML, "persona: realestate") {
		t.Errorf("expected real-estate yaml, got %q", resp.ConfigYAML)
	}
}

func TestGenerateConfig_RSSelf_AcceptsAlias(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	body := []byte(`{"persona":"RS-Self"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/generate-config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp generateConfigResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Persona != "rs-self" {
		t.Errorf("expected normalised persona=rs-self, got %q", resp.Persona)
	}
}

func TestGenerateConfig_BadInput(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	cases := []struct {
		name string
		body string
		want int
	}{
		{"empty body", "", http.StatusBadRequest},
		{"non-JSON", "not json", http.StatusBadRequest},
		{"unknown persona", `{"persona":"frontier"}`, http.StatusBadRequest},
		{"missing persona", `{}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var body io.Reader
			if tc.body != "" {
				body = strings.NewReader(tc.body)
			}
			req := httptest.NewRequest(http.MethodPost, "/api/onboarding/generate-config", body)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Errorf("expected %d, got %d body=%s", tc.want, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestActivateConfig_NoDB_503(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	body := []byte(`{"persona":"realestate","config_yaml":"persona: realestate"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/activate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 (pool nil), got %d", rec.Code)
	}
}

func TestFireScript_Stub(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/demo/fire-script", nil))
	if rec.Code != http.StatusNotImplemented {
		t.Errorf("expected 501 stub, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "stub") {
		t.Errorf("expected stub marker, got %s", rec.Body.String())
	}
}

func TestAdminSeed_Stub(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/admin/seed", nil))
	if rec.Code != http.StatusNotImplemented {
		t.Errorf("expected 501 stub, got %d", rec.Code)
	}
}

func TestDemoReset_NoDB_503(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/demo/reset", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}

func TestReplayLastTrigger_NoDB_503(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/demo/replay-last-trigger", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}

func TestAdminCanned_NoDB_503(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/admin/canned?template=foo&persona=realestate", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}

func TestMockEmails_NoDB_503(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/mock-emails", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}

// --- SSE: stream emits flushed payload + heartbeat ---

func TestSSEStream_FlushAndPublish(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	// We need a real http.Server so our handler sees a flusher-capable
	// ResponseWriter (httptest.NewRecorder doesn't implement http.Flusher).
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, httpSrv.URL+"/api/streams/triggers", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET stream: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Errorf("expected text/event-stream, got %q", got)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-cache" {
		t.Errorf("expected Cache-Control: no-cache, got %q", got)
	}

	// Publish a single event after the stream is established. We wait for
	// the connect comment first.
	go func() {
		// Give the handler a moment to register the subscriber.
		time.Sleep(50 * time.Millisecond)
		srv.hub.Publish(sse.StreamTriggers, sse.Message{
			Event: "trigger",
			Data:  map[string]any{"rule_name": "test_rule"},
		})
	}()

	// Read until we see our test trigger or the heartbeat.
	buf := make([]byte, 4096)
	deadline := time.After(2 * time.Second)
	var collected strings.Builder
readLoop:
	for {
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for SSE payload, got: %q", collected.String())
		default:
			n, err := resp.Body.Read(buf)
			if n > 0 {
				collected.Write(buf[:n])
				body := collected.String()
				if strings.Contains(body, "event: trigger") &&
					strings.Contains(body, "test_rule") {
					break readLoop
				}
			}
			if err != nil {
				if collected.Len() == 0 {
					t.Fatalf("unexpected read error: %v", err)
				}
				return
			}
		}
	}
}

// TestSSEStream_EventNameMatchesStreamName verifies that the SSE wire format
// emitted for the "events" stream carries `event: events` — matching the name
// the frontend's addEventListener registers. This is the regression test for
// the singular/plural mismatch bug.
func TestSSEStream_EventNameMatchesStreamName(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, httpSrv.URL+"/api/streams/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET stream: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Publish a message with Event set to the stream name (the correct value
	// after the fix). The raw wire format must contain "event: events".
	go func() {
		time.Sleep(50 * time.Millisecond)
		srv.hub.Publish(sse.StreamEvents, sse.Message{
			Event: sse.StreamEvents,
			Data:  map[string]any{"event_type": "page"},
		})
	}()

	buf := make([]byte, 4096)
	deadline := time.After(2 * time.Second)
	var collected strings.Builder
readLoop:
	for {
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for SSE payload, got: %q", collected.String())
		default:
			n, readErr := resp.Body.Read(buf)
			if n > 0 {
				collected.Write(buf[:n])
				body := collected.String()
				if strings.Contains(body, "event: events") &&
					strings.Contains(body, "event_type") {
					break readLoop
				}
			}
			if readErr != nil {
				if collected.Len() == 0 {
					t.Fatalf("unexpected read error: %v", readErr)
				}
				return
			}
		}
	}

	// Negative assertion: the broken singular form must NOT appear.
	if strings.Contains(collected.String(), "event: event\n") {
		t.Errorf("found broken singular 'event: event' in wire output — event name mismatch not fixed")
	}
}

func TestSSEStream_UnknownStreamReturns404(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/streams/no-such-stream", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// --- SeedFS path traversal protection ---

func TestDiskSeedFS_RejectsTraversal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fs := NewDiskSeedFS(dir)
	cases := []string{"../etc/passwd", "/etc/passwd", "a/../../b"}
	for _, c := range cases {
		_, err := fs.ReadFile(c)
		if err == nil {
			t.Errorf("expected error for traversal path %q", c)
		}
	}
}

// --- Smoke counter to guarantee the test list is non-empty post-refactor ---

func TestServer_HasMetrics(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	srv.Handler().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if got := totalReqCount(srv); got == 0 {
		t.Errorf("expected metrics counter > 0")
	}
}

func totalReqCount(s *Server) uint64 {
	var sum uint64
	s.metrics.mu.RLock()
	defer s.metrics.mu.RUnlock()
	for _, c := range s.metrics.reqs {
		sum += c.Load()
	}
	return sum
}

// (atomic import retained so go vet doesn't complain in stub builds)
var _ atomic.Uint64
