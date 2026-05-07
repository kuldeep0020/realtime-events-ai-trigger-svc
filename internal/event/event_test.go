package event_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/event"
)

// roundTripJSON verifies that an Event can be marshaled to JSON and back without
// data loss on the key fields consumed by other work packages.
func TestEventRoundTrip(t *testing.T) {
	original := event.Event{
		Type:              "track",
		Channel:           "browser",
		Event:             "Listing Viewed",
		AnonymousID:       "anon_demo-re-001",
		MessageID:         "test-msg-001",
		OriginalTimestamp: time.Date(2026, 5, 7, 8, 14, 0, 0, time.UTC),
		Context: event.Context{
			Library:  event.Library{Name: "analytics-js", Version: "3.5.0"},
			Page:     event.Page{Path: "/listings/L107", URL: "https://realestate-demo.example/listings/L107"},
			Locale:   "en-IN",
			Timezone: "Asia/Kolkata",
		},
		Properties: json.RawMessage(`{"listing_id":"L107","price":1500000}`),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded event.Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.AnonymousID != original.AnonymousID {
		t.Errorf("AnonymousID: got %q, want %q", decoded.AnonymousID, original.AnonymousID)
	}
	if decoded.PagePath() != "/listings/L107" {
		t.Errorf("PagePath: got %q", decoded.PagePath())
	}
	if decoded.HasErrorEventName() {
		t.Error("HasErrorEventName should be false for 'Listing Viewed'")
	}
}

func TestHasErrorEventName(t *testing.T) {
	cases := []struct {
		name  string
		event string
		want  bool
	}{
		{"destination error", "Destination Setup Error", true},
		{"source error", "Source Setup Error", true},
		{"webhook error", "Webhook Send Error", true},
		{"normal track", "Listing Viewed", false},
		{"account created", "Account Created", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		e := event.Event{Event: tc.event}
		if got := e.HasErrorEventName(); got != tc.want {
			t.Errorf("[%s] HasErrorEventName()=%v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestIsConsentBlocked(t *testing.T) {
	blocked := event.Event{
		Context: event.Context{
			ConsentManagement: &event.ConsentManagement{
				DeniedConsentIds: []string{"analytics"},
			},
		},
	}
	if !blocked.IsConsentBlocked() {
		t.Error("expected IsConsentBlocked=true for event with deniedConsentIds")
	}

	clean := event.Event{}
	if clean.IsConsentBlocked() {
		t.Error("expected IsConsentBlocked=false for event with no consent management")
	}
}

func TestPropertiesMap(t *testing.T) {
	e := event.Event{
		Properties: json.RawMessage(`{"price":1500000,"bedrooms":4}`),
	}
	m := e.PropertiesMap()
	if m == nil {
		t.Fatal("PropertiesMap returned nil")
	}
	if m["bedrooms"] != float64(4) {
		t.Errorf("bedrooms: got %v", m["bedrooms"])
	}
}

func TestEffectiveAnonymousID(t *testing.T) {
	e := event.Event{UserID: "demo-rs-001"}
	if got := e.EffectiveAnonymousID(); got != "demo-rs-001" {
		t.Errorf("fallback to UserID: got %q", got)
	}

	e.AnonymousID = "anon-123"
	if got := e.EffectiveAnonymousID(); got != "anon-123" {
		t.Errorf("prefer AnonymousID: got %q", got)
	}
}
