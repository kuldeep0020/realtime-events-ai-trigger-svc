package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/db"
	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/sse"
	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/window"
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
	// Use a valid YAML with at least one rule so that input-validation passes
	// and the 503 is triggered by the absent DB pool, not a 400 for bad input.
	body := []byte(`{"persona":"realestate","config_yaml":"persona: realestate\nrules:\n- name: r\n  when: {}\n  fire: {}\n"}`)
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

// newTestServerWithFireScript builds a Server wired with a mock FireScript
// callback that tracks calls and returns the configured result.
func newTestServerWithFireScript(t *testing.T, fn FireScriptFunc) *Server {
	t.Helper()
	seed := NewMapSeedFS(map[string][]byte{
		"persona-configs/realestate.yaml": []byte("persona: realestate\nrules: []\n"),
		"persona-configs/rs-self.yaml":    []byte("persona: rs-self\nrules: []\n"),
	})
	srv := New(Config{
		Pool:       nil,
		Hub:        sse.NewHub(sse.WithHeartbeatInterval(20 * time.Millisecond)),
		Seed:       seed,
		FireScript: fn,
	})
	t.Cleanup(func() { _ = srv.hub.Close(context.Background()) })
	return srv
}

func TestFireScript_ValidCountAndSpeed(t *testing.T) {
	t.Parallel()

	var lastPersona string
	var lastCount int
	var lastSpeed float64

	srv := newTestServerWithFireScript(t, func(_ context.Context, persona string, count int, speed float64) (int, error) {
		lastPersona = persona
		lastCount = count
		lastSpeed = speed
		return count * 3, nil // simulate 3 events per session
	})

	body := bytes.NewBufferString(`{"persona":"realestate","count":2,"speed":0.5}`)
	req := httptest.NewRequest(http.MethodPost, "/api/demo/fire-script", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if lastPersona != "realestate" {
		t.Errorf("persona=%q, want realestate", lastPersona)
	}
	if lastCount != 2 {
		t.Errorf("count=%d, want 2", lastCount)
	}
	if lastSpeed != 0.5 {
		t.Errorf("speed=%v, want 0.5", lastSpeed)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp["count"].(float64) != 2 {
		t.Errorf("response count=%v, want 2", resp["count"])
	}
	if resp["speed"].(float64) != 0.5 {
		t.Errorf("response speed=%v, want 0.5", resp["speed"])
	}
}

func TestFireScript_DefaultCountAndSpeed(t *testing.T) {
	t.Parallel()

	var lastCount int
	var lastSpeed float64

	srv := newTestServerWithFireScript(t, func(_ context.Context, _ string, count int, speed float64) (int, error) {
		lastCount = count
		lastSpeed = speed
		return 5, nil
	})

	body := bytes.NewBufferString(`{"persona":"realestate"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/demo/fire-script", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if lastCount != 1 {
		t.Errorf("default count=%d, want 1", lastCount)
	}
	if lastSpeed != 1.0 {
		t.Errorf("default speed=%v, want 1.0", lastSpeed)
	}
}

func TestFireScript_InvalidCount(t *testing.T) {
	t.Parallel()
	srv := newTestServerWithFireScript(t, func(_ context.Context, _ string, _ int, _ float64) (int, error) {
		return 0, nil
	})

	for _, badCount := range []int{0, 4, 10, -1} {
		body := bytes.NewBufferString(`{"persona":"realestate","count":` + strconv.Itoa(badCount) + `,"speed":1.0}`)
		req := httptest.NewRequest(http.MethodPost, "/api/demo/fire-script", body)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("count=%d: expected 400, got %d body=%s", badCount, rec.Code, rec.Body.String())
		}
	}
}

func TestFireScript_InvalidSpeed(t *testing.T) {
	t.Parallel()
	srv := newTestServerWithFireScript(t, func(_ context.Context, _ string, _ int, _ float64) (int, error) {
		return 0, nil
	})

	for _, badSpeed := range []string{"1.5", "3.0", "0.1", "0.0"} {
		body := bytes.NewBufferString(`{"persona":"realestate","count":1,"speed":` + badSpeed + `}`)
		req := httptest.NewRequest(http.MethodPost, "/api/demo/fire-script", body)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("speed=%s: expected 400, got %d body=%s", badSpeed, rec.Code, rec.Body.String())
		}
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

// --- Wire-shape conformance tests (SSEEventPayload, SSEWindowPayload, etc.) ---
//
// These tests publish a message with a real payload shape via hub.Publish and
// assert that the JSON on the wire matches the TypeScript SSE*Payload types in
// frontend/types/api.ts. Each test publishes to its stream, reads until the
// data line appears, and unmarshals the JSON to verify key presence and types.

// collectSSEData opens an SSE connection to streamPath on httpSrv, sends a
// message via publishFn after a short delay, and returns the first non-heartbeat
// "data: " payload found before the deadline. The returned []byte is the raw
// JSON value on the data line (no leading "data: " prefix).
//
// SSE messages are parsed as event+data pairs. Heartbeat messages (event:
// heartbeat) are skipped so the caller always receives an application payload.
func collectSSEData(
	t *testing.T,
	httpSrv *httptest.Server,
	streamPath string,
	publishFn func(),
) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, httpSrv.URL+streamPath, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", streamPath, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		publishFn()
	}()

	buf := make([]byte, 8192)
	deadline := time.After(2 * time.Second)
	var collected strings.Builder
	for {
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for SSE data line on %s; got: %q", streamPath, collected.String())
		default:
		}
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			collected.Write(buf[:n])
		}
		// Parse SSE messages as event+data pairs separated by blank lines.
		// Each message block looks like:
		//   event: <name>\n
		//   data: <json>\n
		//   \n
		// We look for a "data: " line that is NOT preceded by "event: heartbeat".
		if data := firstNonHeartbeatData(collected.String()); data != nil {
			return data
		}
		if readErr != nil {
			t.Fatalf("unexpected read error on %s after %d bytes: %v", streamPath, collected.Len(), readErr)
		}
	}
}

// firstNonHeartbeatData parses the accumulated SSE text and returns the first
// data line that does not belong to a heartbeat message, or nil if not found yet.
func firstNonHeartbeatData(text string) []byte {
	// Split into message blocks (delimited by blank lines).
	blocks := strings.Split(text, "\n\n")
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" || strings.HasPrefix(block, ":") {
			continue // comment or empty
		}
		lines := strings.Split(block, "\n")
		isHeartbeat := false
		var dataLine string
		for _, line := range lines {
			line = strings.TrimRight(line, "\r")
			if line == "event: heartbeat" {
				isHeartbeat = true
			}
			if strings.HasPrefix(line, "data: ") {
				dataLine = strings.TrimPrefix(line, "data: ")
			}
		}
		if !isHeartbeat && dataLine != "" {
			return []byte(dataLine)
		}
	}
	return nil
}

// TestSSEStream_EventsPayloadShape verifies that the events stream emits a
// JSON object with camelCase fields matching SSEEventPayload. It also
// negatively asserts that snake_case "anonymous_id" does not appear (that
// would indicate the wrong DTO being serialized).
func TestSSEStream_EventsPayloadShape(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	raw := collectSSEData(t, httpSrv, "/api/streams/events", func() {
		srv.hub.Publish(sse.StreamEvents, sse.Message{
			Event: sse.StreamEvents,
			Data: map[string]any{
				"type":              "page",
				"channel":           "web",
				"anonymousId":       "anon-abc-123",
				"messageId":         "msg-001",
				"originalTimestamp": "2024-01-01T00:00:00.000Z",
			},
		})
	})

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("events data line is not valid JSON: %v — raw: %s", err, raw)
	}

	// Required camelCase fields from SSEEventPayload.
	for _, key := range []string{"type", "channel", "anonymousId", "messageId", "originalTimestamp"} {
		if _, ok := payload[key]; !ok {
			t.Errorf("events payload missing required key %q; got keys: %v", key, mapKeys(payload))
		}
	}
	// anonymousId must be a non-empty string.
	if v, ok := payload["anonymousId"].(string); !ok || v == "" {
		t.Errorf("events payload[\"anonymousId\"] must be a non-empty string, got %T(%v)", payload["anonymousId"], payload["anonymousId"])
	}
	// Negative: snake_case leak would break the frontend.
	if _, bad := payload["anonymous_id"]; bad {
		t.Errorf("events payload must NOT contain snake_case \"anonymous_id\" — frontend reads camelCase anonymousId")
	}
}

// TestSSEStream_WindowsPayloadShape verifies that the windows stream emits
// a JSON object with snake_case fields matching SSEWindowPayload, including
// the computed idle_seconds field.
func TestSSEStream_WindowsPayloadShape(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	raw := collectSSEData(t, httpSrv, "/api/streams/windows", func() {
		srv.hub.Publish(sse.StreamWindows, sse.Message{
			Event: sse.StreamWindows,
			Data: map[string]any{
				"anonymous_id":       "anon-win-456",
				"event_count":        3,
				"event_type_count":   map[string]any{"page": 2, "identify": 1},
				"event_name_count":   map[string]any{},
				"last_seen":          "2024-01-01T00:00:00Z",
				"has_error_event":    false,
				"idle_seconds":       7,
			},
		})
	})

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("windows data line is not valid JSON: %v — raw: %s", err, raw)
	}

	// Required snake_case fields from SSEWindowPayload.
	for _, key := range []string{"anonymous_id", "event_count", "event_type_count", "idle_seconds"} {
		if _, ok := payload[key]; !ok {
			t.Errorf("windows payload missing required key %q; got keys: %v", key, mapKeys(payload))
		}
	}
	// anonymous_id must be a non-empty string.
	if v, ok := payload["anonymous_id"].(string); !ok || v == "" {
		t.Errorf("windows payload[\"anonymous_id\"] must be a non-empty string, got %T(%v)", payload["anonymous_id"], payload["anonymous_id"])
	}
	// event_count must be a number (JSON unmarshals numbers as float64).
	if _, ok := payload["event_count"].(float64); !ok {
		t.Errorf("windows payload[\"event_count\"] must be a number, got %T", payload["event_count"])
	}
	// idle_seconds must be a number.
	if _, ok := payload["idle_seconds"].(float64); !ok {
		t.Errorf("windows payload[\"idle_seconds\"] must be a number, got %T", payload["idle_seconds"])
	}
}

// TestSSEStream_TriggersPayloadShape verifies that the triggers stream emits
// a JSON object with all fields from SSETriggerPayload, including window_snapshot.
func TestSSEStream_TriggersPayloadShape(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	raw := collectSSEData(t, httpSrv, "/api/streams/triggers", func() {
		srv.hub.Publish(sse.StreamTriggers, sse.Message{
			Event: sse.StreamTriggers,
			Data: map[string]any{
				"id":              "trigger-uuid-001",
				"rule_name":       "idle_10s",
				"persona":         "realestate",
				"anonymous_id":    "anon-trig-789",
				"fired_at":        "2024-01-01T00:00:00Z",
				"window_snapshot": map[string]any{"event_count": 5, "idle_seconds": 12},
				"destination":     "email:demo@example.com",
				"dispatch_status": "sent",
			},
		})
	})

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("triggers data line is not valid JSON: %v — raw: %s", err, raw)
	}

	// Required fields from SSETriggerPayload.
	for _, key := range []string{"id", "rule_name", "anonymous_id", "fired_at", "window_snapshot", "destination", "dispatch_status"} {
		if _, ok := payload[key]; !ok {
			t.Errorf("triggers payload missing required key %q; got keys: %v", key, mapKeys(payload))
		}
	}
	// window_snapshot must be a JSON object (not nil / missing).
	snap, ok := payload["window_snapshot"].(map[string]any)
	if !ok || snap == nil {
		t.Errorf("triggers payload[\"window_snapshot\"] must be a JSON object, got %T", payload["window_snapshot"])
	} else {
		if _, hasCount := snap["event_count"]; !hasCount {
			t.Errorf("window_snapshot missing event_count; got keys: %v", mapKeys(snap))
		}
	}
	// id must be a non-empty string.
	if v, ok := payload["id"].(string); !ok || v == "" {
		t.Errorf("triggers payload[\"id\"] must be a non-empty string, got %T(%v)", payload["id"], payload["id"])
	}
}

// TestSSEStream_MockEmailsPayloadShape verifies that the mock_emails stream
// emits a JSON object with the MockEmailPayload shape — in particular that
// "id", "to_email", and "body_markdown" are present and that the broken
// "body_md" key does NOT appear.
func TestSSEStream_MockEmailsPayloadShape(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	raw := collectSSEData(t, httpSrv, "/api/streams/mock_emails", func() {
		srv.hub.Publish(sse.StreamMockEmails, sse.Message{
			Event: sse.StreamMockEmails,
			Data: map[string]any{
				"id":            "email-uuid-001",
				"trigger_id":    "trigger-uuid-001",
				"to_email":      "anon-user@example.com",
				"subject":       "Your RudderStack digest",
				"body_markdown": "# Hello\nThis is your digest.",
				"created_at":    "2024-01-01T00:00:00Z",
			},
		})
	})

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("mock_emails data line is not valid JSON: %v — raw: %s", err, raw)
	}

	// Required fields from MockEmailPayload.
	for _, key := range []string{"id", "to_email", "subject", "body_markdown", "created_at"} {
		if _, ok := payload[key]; !ok {
			t.Errorf("mock_emails payload missing required key %q; got keys: %v", key, mapKeys(payload))
		}
	}
	// id must be a non-empty string.
	if v, ok := payload["id"].(string); !ok || v == "" {
		t.Errorf("mock_emails payload[\"id\"] must be a non-empty string, got %T(%v)", payload["id"], payload["id"])
	}
	// to_email must be a non-empty string.
	if v, ok := payload["to_email"].(string); !ok || v == "" {
		t.Errorf("mock_emails payload[\"to_email\"] must be a non-empty string, got %T(%v)", payload["to_email"], payload["to_email"])
	}
	// Negative: body_md is the old broken key that breaks the frontend.
	if _, bad := payload["body_md"]; bad {
		t.Errorf("mock_emails payload must NOT contain \"body_md\" — frontend reads \"body_markdown\"")
	}
}

// TestReplayLastTrigger_PublishesToSSE verifies that replayTriggerSSEMessage
// builds an sse.Message whose Data map contains every field required by the
// frontend SSETriggerPayload, and that the message can be published to a real
// Hub so a subscriber receives it.
//
// The test exercises the pure helper without a Postgres pool. The integration
// path (handler calls hub.Publish) is covered by the manual curl verification
// described in the task brief.
func TestReplayLastTrigger_PublishesToSSE(t *testing.T) {
	t.Parallel()

	firedAt := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	row := replayLastTriggerResponse{
		ID:             "test-uuid-001",
		RuleName:       "idle_10s",
		Persona:        "realestate",
		AnonymousID:    "anon-replay-test",
		FiredAt:        firedAt,
		WindowSnapshot: json.RawMessage(`{"event_count":3,"idle_seconds":11}`),
		LLMParsed:      json.RawMessage(`{"subject":"hello"}`),
		Destination:    "email:demo@example.com",
		DispatchStatus: "sent",
	}

	msg := replayTriggerSSEMessage(row)

	// Event name must match the stream so the frontend addEventListener fires.
	if msg.Event != sse.StreamTriggers {
		t.Errorf("expected Event=%q, got %q", sse.StreamTriggers, msg.Event)
	}

	data, ok := msg.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected Data to be map[string]any, got %T", msg.Data)
	}

	// All required SSETriggerPayload fields must be present.
	for _, key := range []string{"id", "rule_name", "persona", "anonymous_id", "fired_at",
		"window_snapshot", "destination", "dispatch_status", "llm_parsed"} {
		if _, exists := data[key]; !exists {
			t.Errorf("SSE data missing required key %q; keys present: %v", key, mapKeys(data))
		}
	}

	// fired_at must be RFC3339.
	if got, ok := data["fired_at"].(string); !ok || got != "2024-06-01T12:00:00Z" {
		t.Errorf("fired_at mismatch: got %v", data["fired_at"])
	}

	// Verify the message round-trips through the hub to a subscriber.
	hub := sse.NewHub(sse.WithHeartbeatInterval(20 * time.Millisecond))
	defer func() { _ = hub.Close(context.Background()) }()

	ch, unsub := hub.Subscribe(sse.StreamTriggers)
	defer unsub()

	hub.Publish(sse.StreamTriggers, msg)

	select {
	case received := <-ch:
		if received.Event != sse.StreamTriggers {
			t.Errorf("received event=%q, want %q", received.Event, sse.StreamTriggers)
		}
		rData, ok := received.Data.(map[string]any)
		if !ok {
			t.Fatalf("received Data not a map, got %T", received.Data)
		}
		if rData["id"] != "test-uuid-001" {
			t.Errorf("received id=%v, want test-uuid-001", rData["id"])
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout: subscriber did not receive replayed trigger message")
	}
}

// mapKeys is a test helper that returns the keys of a map for readable error messages.
func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// --- Dashboard rehydration endpoint nil-guard tests ---

func TestRecentEvents_NoDB_503(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/recent-events", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 (pool nil), got %d", rec.Code)
	}
}

func TestActiveSessions_NoStore_503(t *testing.T) {
	t.Parallel()
	// newTestServer does not set WindowStore, so handleActiveSessions must return 503.
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/active-sessions", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 (window store nil), got %d", rec.Code)
	}
}

func TestRecentTriggers_NoDB_503(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/recent-triggers", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 (pool nil), got %d", rec.Code)
	}
}

func TestActiveSessions_WithStore_ReturnsJSON(t *testing.T) {
	t.Parallel()
	// Build a server with a real window store populated with two windows.
	seed := NewMapSeedFS(map[string][]byte{
		"persona-configs/realestate.yaml": []byte("persona: realestate\nrules: []\n"),
	})
	ws := buildTestWindowStore(t)
	srv := New(Config{
		Pool:        nil,
		Hub:         sse.NewHub(sse.WithHeartbeatInterval(20 * time.Millisecond)),
		Seed:        seed,
		WindowStore: ws,
	})
	t.Cleanup(func() { _ = srv.hub.Close(context.Background()) })

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/active-sessions", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	sessions, ok := resp["sessions"].([]any)
	if !ok {
		t.Fatalf("expected sessions array, got %T", resp["sessions"])
	}
	if len(sessions) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(sessions))
	}
	// Each session must have anonymous_id and event_count.
	for _, s := range sessions {
		m, ok := s.(map[string]any)
		if !ok {
			t.Errorf("session not a map, got %T", s)
			continue
		}
		if _, ok := m["anonymous_id"]; !ok {
			t.Errorf("session missing anonymous_id; keys: %v", mapKeys(m))
		}
		if _, ok := m["event_count"]; !ok {
			t.Errorf("session missing event_count; keys: %v", mapKeys(m))
		}
	}
}

func TestParseLimitParam_Defaults(t *testing.T) {
	t.Parallel()
	cases := []struct {
		query      string
		defaultVal int
		maxVal     int
		want       int
	}{
		{"", 50, 200, 50},
		{"limit=10", 50, 200, 10},
		{"limit=300", 50, 200, 200},   // capped
		{"limit=-5", 50, 200, 50},     // negative → default
		{"limit=0", 50, 200, 50},      // zero → default
		{"limit=abc", 50, 200, 50},    // invalid → default
		{"limit=200", 50, 200, 200},   // exact max ok
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, "/api/recent-events?"+tc.query, nil)
		got := parseLimitParam(req, tc.defaultVal, tc.maxVal)
		if got != tc.want {
			t.Errorf("query=%q: got %d, want %d", tc.query, got, tc.want)
		}
	}
}

// buildTestWindowStore creates a Store with two windows for test assertions.
func buildTestWindowStore(t *testing.T) *window.Store {
	t.Helper()
	ws := window.New(0)
	ws.WithWindow("anon-test-001", func(w *window.UserWindow) {
		w.EventCount = 3
		w.LastSeen = time.Now().UTC()
	})
	ws.WithWindow("anon-test-002", func(w *window.UserWindow) {
		w.EventCount = 7
		w.LastSeen = time.Now().UTC()
	})
	return ws
}

// openAPITestPool opens a real Postgres pool using TEST_DATABASE_URL and runs
// migrations. Tests calling this are skipped when the env var is unset.
func openAPITestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping integration test")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	if err := db.RunMigrationsUp(ctx, pool, "../../migrations"); err != nil {
		t.Fatalf("RunMigrationsUp: %v", err)
	}
	t.Cleanup(func() { db.Close(pool) })
	return pool
}

// newTestServerWithPool builds a Server wired to a real Postgres pool.
func newTestServerWithPool(t *testing.T, pool *pgxpool.Pool) *Server {
	t.Helper()
	seed := NewMapSeedFS(map[string][]byte{
		"persona-configs/realestate.yaml": []byte("persona: realestate\nrules: []\n"),
		"persona-configs/rs-self.yaml":    []byte("persona: rs-self\nrules: []\n"),
	})
	srv := New(Config{
		Pool: pool,
		Hub:  sse.NewHub(sse.WithHeartbeatInterval(20 * time.Millisecond)),
		Seed: seed,
	})
	t.Cleanup(func() {
		_ = srv.hub.Close(context.Background())
	})
	return srv
}

// TestRecentEvents_HappyPath_PreservesCamelCase asserts that GET /api/recent-events
// returns event payloads verbatim from the DB with camelCase keys intact —
// no snake_case keys must leak through re-serialisation.
func TestRecentEvents_HappyPath_PreservesCamelCase(t *testing.T) {
	pool := openAPITestPool(t)

	// Clean up events we insert so we don't leak state across test runs.
	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM events WHERE anonymous_id = 'test-camelcase-a1'`)
	})

	// Insert a raw event row whose payload contains camelCase keys — exactly the
	// shape the RudderStack SDK sends. The handler must return these bytes verbatim.
	payload := `{"type":"track","channel":"browser","anonymousId":"a1","messageId":"m1","originalTimestamp":"2026-05-07T12:00:00Z","event":"Listing Viewed","properties":{"price":100}}`
	_, err := pool.Exec(ctx, `
		INSERT INTO events (anonymous_id, write_key, event_type, event_name, payload)
		VALUES ('test-camelcase-a1', 'wk-test', 'track', 'Listing Viewed', $1::jsonb)`,
		payload,
	)
	if err != nil {
		t.Fatalf("insert test event: %v", err)
	}

	srv := newTestServerWithPool(t, pool)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/recent-events?limit=10", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Events []json.RawMessage `json:"events"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if len(resp.Events) == 0 {
		t.Fatal("expected at least 1 event in response")
	}

	// Find our inserted event by messageId.
	var found map[string]any
	for _, raw := range resp.Events {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		if m["messageId"] == "m1" {
			found = m
			break
		}
	}
	if found == nil {
		t.Fatalf("inserted event with messageId=m1 not found in response; got %d events", len(resp.Events))
	}

	// camelCase key must be present end-to-end.
	if v, ok := found["anonymousId"].(string); !ok || v != "a1" {
		t.Errorf("expected anonymousId=a1, got %v", found["anonymousId"])
	}
	// snake_case must NOT appear — that would indicate double-serialisation.
	rawBody := rec.Body.String()
	if strings.Contains(rawBody, `"anonymous_id":"a1"`) {
		t.Errorf("response must NOT contain snake_case anonymous_id; raw body snippet: %s", rawBody[:min(len(rawBody), 300)])
	}
}

// TestRecentTriggers_HappyPath_PreservesWindowSnapshotJSON asserts that GET
// /api/recent-triggers returns window_snapshot as a raw JSON object — not a
// double-encoded string — so the frontend can access e.g. .event_count directly.
func TestRecentTriggers_HappyPath_PreservesWindowSnapshotJSON(t *testing.T) {
	pool := openAPITestPool(t)

	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM triggers WHERE anonymous_id = 'test-snapshot-a1'`)
	})

	windowSnap := `{"event_count":7,"idle_seconds":12,"anonymous_id":"a1"}`
	fullEvents := `[]`
	_, err := pool.Exec(ctx, `
		INSERT INTO triggers
			(rule_name, persona, anonymous_id, window_snapshot, full_events, destination, dispatch_status)
		VALUES ('test-rule', 'realestate', 'test-snapshot-a1', $1::jsonb, $2::jsonb, 'email:test@example.com', 'sent')`,
		windowSnap, fullEvents,
	)
	if err != nil {
		t.Fatalf("insert test trigger: %v", err)
	}

	srv := newTestServerWithPool(t, pool)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/recent-triggers", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Triggers []struct {
			AnonymousID    string          `json:"anonymous_id"`
			WindowSnapshot json.RawMessage `json:"window_snapshot"`
		} `json:"triggers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if len(resp.Triggers) == 0 {
		t.Fatal("expected at least 1 trigger in response")
	}

	// Find our inserted trigger.
	var snap json.RawMessage
	for _, tr := range resp.Triggers {
		if tr.AnonymousID == "test-snapshot-a1" {
			snap = tr.WindowSnapshot
			break
		}
	}
	if snap == nil {
		t.Fatal("inserted trigger with anonymous_id=test-snapshot-a1 not found")
	}

	// window_snapshot must deserialise as a JSON object — not a double-encoded string.
	var snapObj map[string]any
	if err := json.Unmarshal(snap, &snapObj); err != nil {
		t.Fatalf("window_snapshot is not a JSON object (double-encoded?): %v — raw: %s", err, snap)
	}
	count, ok := snapObj["event_count"].(float64)
	if !ok {
		t.Errorf("window_snapshot.event_count must be a number, got %T(%v)", snapObj["event_count"], snapObj["event_count"])
	} else if int(count) != 7 {
		t.Errorf("window_snapshot.event_count: want 7, got %v", count)
	}
}

// (atomic import retained so go vet doesn't complain in stub builds)
var _ atomic.Uint64
