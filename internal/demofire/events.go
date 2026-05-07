// Package demofire constructs and fires the persona-specific browser-channel
// event sequences described in §6.2 / §6.3 of the design doc.
//
// Each script is a slice of ScriptSteps; each step is (DelayMs, Event).
// The Firer walks the slice, sleeps per DelayMs, and POSTs each event as a
// single-event {batch:[…]} body to {INGESTION_URL}/v1/batch with HTTP Basic
// auth derived from the persona's writeKey.
//
// We deliberately wrap RudderStack's v3 JS-SDK on-the-wire shape (see
// internal/event for the canonical type) rather than a slimmer payload —
// the consumer downstream parses the full Event so verifying it round-trips
// through the demo path is itself a useful smoke test.
package demofire

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/event"
)

// ScriptStep pairs a relative delay with a fully-formed event.
type ScriptStep struct {
	// DelayMs is the wall-clock delay BEFORE this step is sent, measured
	// from the previous step (or from t=0 for the first step).
	DelayMs int
	// Event is the canonical browser-channel event payload. Timestamps are
	// stamped to time.Now at firing time; do NOT pre-populate them.
	Event event.Event
}

// builderOpts collects the per-event metadata the helpers in this file
// need to stamp consistent timestamps and message IDs.
type builderOpts struct {
	now func() time.Time
	id  func() string
}

func defaultOpts() builderOpts {
	return builderOpts{
		now: time.Now,
		id:  newMessageID,
	}
}

// newMessageID returns a fresh 32-char hex string. We use crypto/rand
// rather than uuid for a smaller dependency surface; downstream filters
// only need uniqueness, not RFC 4122 compliance.
func newMessageID() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}

// ----------------------------------------------------------------------------
// Real-estate event builders (browser channel)
// ----------------------------------------------------------------------------

// reAnonID is the fixed anonymousId used by the real-estate demo. Matches
// seed/mock_profiles.yaml so trait enrichment produces a hit.
const reAnonID = "anon_demo-re-001"

// rsAnonID is the (anonymousId, userId) value for the rs-self demo.
const rsAnonID = "demo-rs-001"

// reBaseContext returns the on-the-wire SDK context for a real-estate
// browser event. Page is overridden per-step.
func reBaseContext(page event.Page) event.Context {
	return event.Context{
		Library: event.Library{Name: "analytics-js", Version: "3.5.0"},
		Page:    page,
		Campaign: map[string]string{
			"utm_source":   "google",
			"utm_medium":   "cpc",
			"utm_campaign": "spring-2026-suburb1",
		},
		OS:           event.OS{Name: "macOS", Version: "13.0"},
		Screen:       event.Screen{Width: 1440, Height: 900, Density: 2, InnerWidth: 1440, InnerHeight: 874},
		UserAgent:    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		Locale:       "en-IN",
		Timezone:     "Asia/Kolkata",
		SessionID:    1714978800000,
		SessionStart: false,
		Traits:       map[string]any{"membership_tier": "browse"},
	}
}

// rsBaseContext returns the on-the-wire SDK context for an rs-self
// onboarding browser event. Page paths walk through the dashboard wizard.
func rsBaseContext(page event.Page) event.Context {
	return event.Context{
		Library:      event.Library{Name: "analytics-js", Version: "3.5.0"},
		Page:         page,
		OS:           event.OS{Name: "macOS", Version: "13.0"},
		Screen:       event.Screen{Width: 1512, Height: 982, Density: 2, InnerWidth: 1512, InnerHeight: 870},
		UserAgent:    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		Locale:       "en-US",
		Timezone:     "America/Los_Angeles",
		SessionID:    1714978800000,
		SessionStart: false,
	}
}

// newIdentifyEvent constructs an identify browser event with the provided
// userId/anonymousId and traits. Traits are JSON-marshalled into the raw
// Traits field on the event.
func newIdentifyEvent(anonID, userID string, traits map[string]any, ctx event.Context, opts builderOpts) event.Event {
	t := opts.now().UTC()
	traitsJSON, _ := json.Marshal(traits)
	return event.Event{
		Type:              "identify",
		Channel:           "browser",
		AnonymousID:       anonID,
		UserID:            userID,
		MessageID:         opts.id(),
		OriginalTimestamp: t,
		SentAt:            t,
		Context:           ctx,
		Traits:            traitsJSON,
		Integrations:      json.RawMessage(`{"All":true}`),
	}
}

// newPageEvent constructs a page event with the given page metadata.
func newPageEvent(anonID, userID string, ctx event.Context, opts builderOpts) event.Event {
	t := opts.now().UTC()
	return event.Event{
		Type:              "page",
		Channel:           "browser",
		AnonymousID:       anonID,
		UserID:            userID,
		MessageID:         opts.id(),
		OriginalTimestamp: t,
		SentAt:            t,
		Context:           ctx,
		Properties:        json.RawMessage(`{}`),
		Integrations:      json.RawMessage(`{"All":true}`),
	}
}

// newTrackEvent constructs a track event with the given name + properties.
func newTrackEvent(anonID, userID, name string, props map[string]any, ctx event.Context, opts builderOpts) event.Event {
	t := opts.now().UTC()
	propsJSON, _ := json.Marshal(props)
	return event.Event{
		Type:              "track",
		Channel:           "browser",
		Event:             name,
		AnonymousID:       anonID,
		UserID:            userID,
		MessageID:         opts.id(),
		OriginalTimestamp: t,
		SentAt:            t,
		Context:           ctx,
		Properties:        propsJSON,
		Integrations:      json.RawMessage(`{"All":true}`),
	}
}
