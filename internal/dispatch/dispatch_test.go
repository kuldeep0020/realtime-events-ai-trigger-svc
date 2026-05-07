package dispatch

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// stubPayload is a minimal in-memory ActionPayload used throughout the tests.
type stubPayload struct {
	template string
	parsed   map[string]any
	raw      string
}

func (s *stubPayload) Template() string         { return s.template }
func (s *stubPayload) Parsed() map[string]any   { return s.parsed }
func (s *stubPayload) Raw() string              { return s.raw }

// realestatePayload returns a canonical real-estate canned payload (matches
// seed/canned-responses-hand.yaml realestate_realtor_pitch).
func realestatePayload() *stubPayload {
	parsed := map[string]any{
		"headline": "Anonymous visitor abandoned a high-intent session in Suburb 1",
		"talking_points": []any{
			"Viewed 3 listings in suburb-1",
			"Applied filter beds_min=3",
			"Spent 22s on L112 detail page",
		},
		"best_cta":         "Call within 30 minutes; lead with L112",
		"urgency":          "high",
		"assigned_realtor": "Priya N.",
	}
	raw, _ := json.Marshal(parsed)
	return &stubPayload{
		template: "realestate_realtor_pitch",
		parsed:   parsed,
		raw:      string(raw),
	}
}

// --- Slack: success path ---

func TestSlackBackend_DispatchSuccess(t *testing.T) {
	t.Parallel()

	var (
		gotBody []byte
		gotCT   string
		hits    int32
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = b
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	b := NewSlackBackend(srv.URL)
	b.sleepFn = func(time.Duration) {} // no real sleeping
	status, finalURL, err := b.Dispatch(context.Background(), "realestate", realestatePayload())

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if status != "sent" {
		t.Fatalf("expected status=sent, got %q", status)
	}
	if finalURL != srv.URL {
		t.Fatalf("expected finalURL=%s, got %q", srv.URL, finalURL)
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("expected exactly 1 webhook call, got %d", hits)
	}
	if gotCT != "application/json" {
		t.Fatalf("expected JSON content-type, got %q", gotCT)
	}

	// Validate the body parses to a Block Kit message with header + section + divider + context.
	var got map[string]any
	if err := json.Unmarshal(gotBody, &got); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	blocks, ok := got["blocks"].([]any)
	if !ok || len(blocks) < 4 {
		t.Fatalf("expected at least 4 blocks in body, got: %v", got)
	}
	header := blocks[0].(map[string]any)
	if header["type"] != "header" {
		t.Errorf("expected first block type=header, got %v", header["type"])
	}
	section := blocks[1].(map[string]any)
	if section["type"] != "section" {
		t.Errorf("expected second block type=section, got %v", section["type"])
	}
	divider := blocks[2].(map[string]any)
	if divider["type"] != "divider" {
		t.Errorf("expected third block type=divider, got %v", divider["type"])
	}
	contextBlock := blocks[3].(map[string]any)
	if contextBlock["type"] != "context" {
		t.Errorf("expected fourth block type=context, got %v", contextBlock["type"])
	}
	// Section text should contain bullets and the CTA.
	sectionText := section["text"].(map[string]any)
	textStr := sectionText["text"].(string)
	if !strings.Contains(textStr, "•") {
		t.Errorf("expected bullets in section, got %q", textStr)
	}
	if !strings.Contains(textStr, "Best CTA:") {
		t.Errorf("expected best CTA in section, got %q", textStr)
	}
}

// --- Slack: retries then succeeds ---

func TestSlackBackend_DispatchRetryThenSuccess(t *testing.T) {
	t.Parallel()

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		if n < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var slept []time.Duration
	var sleepMu sync.Mutex

	b := NewSlackBackend(srv.URL)
	b.sleepFn = func(d time.Duration) {
		sleepMu.Lock()
		slept = append(slept, d)
		sleepMu.Unlock()
	}

	status, _, err := b.Dispatch(context.Background(), "realestate", realestatePayload())
	if err != nil {
		t.Fatalf("expected eventual success, got error: %v", err)
	}
	if status != "sent" {
		t.Fatalf("expected status=sent after retry, got %q", status)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Fatalf("expected 2 hits (1 fail, 1 success), got %d", got)
	}
	sleepMu.Lock()
	defer sleepMu.Unlock()
	if len(slept) != 1 || slept[0] != 200*time.Millisecond {
		t.Errorf("expected single 200ms sleep before attempt 2, got %v", slept)
	}
}

// --- Slack: 3 failures, all attempts exhausted ---

func TestSlackBackend_DispatchAllFailures(t *testing.T) {
	t.Parallel()

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("nope"))
	}))
	defer srv.Close()

	var slept []time.Duration
	b := NewSlackBackend(srv.URL)
	b.sleepFn = func(d time.Duration) { slept = append(slept, d) }

	status, finalURL, err := b.Dispatch(context.Background(), "realestate", realestatePayload())
	if err == nil {
		t.Fatalf("expected error after all retries, got nil")
	}
	if status != "failed" {
		t.Fatalf("expected status=failed, got %q", status)
	}
	if finalURL != "" {
		t.Errorf("expected empty finalURL on failure, got %q", finalURL)
	}
	if got := atomic.LoadInt32(&hits); got != int32(slackMaxAttempts) {
		t.Fatalf("expected %d hits (all retries), got %d", slackMaxAttempts, got)
	}
	// Sleeps before attempts 2 and 3 only.
	wantSleeps := []time.Duration{200 * time.Millisecond, 400 * time.Millisecond}
	if len(slept) != len(wantSleeps) {
		t.Fatalf("expected %d sleeps, got %d (%v)", len(wantSleeps), len(slept), slept)
	}
	for i, want := range wantSleeps {
		if slept[i] != want {
			t.Errorf("slept[%d]=%v, want %v", i, slept[i], want)
		}
	}
}

// --- Slack: empty webhook URL ---

func TestSlackBackend_EmptyWebhookURL(t *testing.T) {
	t.Parallel()
	b := NewSlackBackend("")
	status, _, err := b.Dispatch(context.Background(), "realestate", realestatePayload())
	if err == nil {
		t.Fatal("expected error for empty webhook URL")
	}
	if status != "failed" {
		t.Errorf("expected status=failed, got %q", status)
	}
}

// --- Slack: context cancelled mid-retry ---

func TestSlackBackend_ContextCancelledDuringRetry(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())

	b := NewSlackBackend(srv.URL)
	b.sleepFn = func(time.Duration) {
		cancel() // cancel during the very first backoff
	}

	status, _, err := b.Dispatch(ctx, "realestate", realestatePayload())
	if err == nil {
		t.Fatal("expected context-cancelled error")
	}
	if status != "failed" {
		t.Errorf("expected status=failed, got %q", status)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected wrapped context.Canceled, got %v", err)
	}
}

// --- Dispatcher: routing + malformed destinations ---

type stubBackend struct {
	persona      string
	gotPersona   string
	gotPayload   ActionPayload
	returnStatus string
	returnURL    string
	returnErr    error
}

func (s *stubBackend) Dispatch(_ context.Context, persona string, payload ActionPayload) (string, string, error) {
	s.gotPersona = persona
	s.gotPayload = payload
	return s.returnStatus, s.returnURL, s.returnErr
}

func TestDispatcher_RoutesByScheme(t *testing.T) {
	t.Parallel()

	slack := &stubBackend{returnStatus: "sent", returnURL: "slack://ok"}
	email := &stubBackend{returnStatus: "sent", returnURL: "/api/mock-emails?to=demo"}

	d := New()
	d.Register("slack", slack)
	d.Register("email", email)

	// slack route
	status, finalURL, err := d.Dispatch(context.Background(), "slack:#realestate", realestatePayload(), "realestate", "anon-1", "rule-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "sent" || finalURL != "slack://ok" {
		t.Errorf("slack route: status=%q url=%q", status, finalURL)
	}
	if slack.gotPayload == nil {
		t.Errorf("slack backend did not receive payload")
	}

	// email route
	status, finalURL, err = d.Dispatch(context.Background(), "email:user", realestatePayload(), "rs-self", "anon-2", "rule-2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "sent" || finalURL != "/api/mock-emails?to=demo" {
		t.Errorf("email route: status=%q url=%q", status, finalURL)
	}
}

func TestDispatcher_MalformedDestination(t *testing.T) {
	t.Parallel()

	d := New()
	d.Register("slack", &stubBackend{})

	cases := []string{"", "noColon", ":noScheme", "trailing:"}
	for _, dst := range cases {
		_, _, err := d.Dispatch(context.Background(), dst, realestatePayload(), "p", "a", "r")
		if err == nil {
			t.Errorf("expected error for destination %q", dst)
		}
	}
}

func TestDispatcher_UnknownScheme(t *testing.T) {
	t.Parallel()

	d := New()
	d.Register("slack", &stubBackend{})

	_, _, err := d.Dispatch(context.Background(), "carrier-pigeon:nest-3", realestatePayload(), "p", "a", "r")
	if err == nil {
		t.Fatal("expected error for unknown scheme")
	}
}

func TestDispatcher_NilPayload(t *testing.T) {
	t.Parallel()
	d := New()
	d.Register("slack", &stubBackend{returnStatus: "sent"})
	_, _, err := d.Dispatch(context.Background(), "slack:#x", nil, "p", "a", "r")
	if err == nil {
		t.Fatal("expected error for nil payload")
	}
}

// --- Email backend: builder logic without DB ---

func TestEncodeLinks_EmptyAndPresent(t *testing.T) {
	t.Parallel()
	if got := encodeLinks(nil); got != nil {
		t.Errorf("encodeLinks(nil): expected nil, got %v", got)
	}
	if got := encodeLinks(map[string]any{}); got != nil {
		t.Errorf("encodeLinks(empty): expected nil, got %v", got)
	}
	in := map[string]any{
		"doc_links": []any{
			map[string]any{"title": "Doc 1", "url": "https://example.com/1"},
		},
	}
	got := encodeLinks(in)
	if got == nil {
		t.Fatal("expected non-nil bytes")
	}
	var out []map[string]any
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatalf("encoded links not valid JSON: %v", err)
	}
	if len(out) != 1 || out[0]["title"] != "Doc 1" {
		t.Errorf("unexpected encoded links: %v", out)
	}
}

// --- Block Kit JSON validity for generic fallback ---

func TestBuildGenericBlocks_ValidJSON(t *testing.T) {
	t.Parallel()
	body, err := buildGenericBlocks("rs_onboarding_stuck", map[string]any{
		"subject": "Stuck on the Amplitude API key error?",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	blocks, ok := got["blocks"].([]any)
	if !ok || len(blocks) < 2 {
		t.Fatalf("expected >= 2 blocks, got: %v", got)
	}
}

// --- Truncation helpers ---

func TestTruncate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in     string
		max    int
		wantEq string
	}{
		{"hello", 5, "hello"},
		{"hello", 3, "hel"},
		{"hello world", 8, "hello..."},
		{"", 5, ""},
	}
	for _, tc := range cases {
		if got := truncate(tc.in, tc.max); got != tc.wantEq {
			t.Errorf("truncate(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.wantEq)
		}
	}
}
