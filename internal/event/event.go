// Package event defines the canonical RudderEvent struct matching the v3 JS-SDK
// on-the-wire shape (browser channel). All other packages consume this type.
package event

import (
	"encoding/json"
	"strings"
	"time"
)

// Event is the canonical RudderStack browser-channel event. Field names and JSON
// tags match the v3 analytics-js SDK wire format (§6.4 of the design doc).
type Event struct {
	Type              string          `json:"type"`
	Channel           string          `json:"channel"`
	Event             string          `json:"event,omitempty"`
	AnonymousID       string          `json:"anonymousId"`
	UserID            string          `json:"userId,omitempty"`
	MessageID         string          `json:"messageId"`
	OriginalTimestamp time.Time       `json:"originalTimestamp"`
	SentAt            time.Time       `json:"sentAt,omitempty"`
	Context           Context         `json:"context"`
	Properties        json.RawMessage `json:"properties,omitempty"`
	Traits            json.RawMessage `json:"traits,omitempty"`
	GroupID           string          `json:"groupId,omitempty"`
	PreviousID        string          `json:"previousId,omitempty"`
	Integrations      json.RawMessage `json:"integrations,omitempty"`
}

// Context holds the SDK environment metadata bundled with every event.
type Context struct {
	Library           Library               `json:"library,omitempty"`
	Page              Page                  `json:"page,omitempty"`
	Campaign          map[string]string     `json:"campaign,omitempty"`
	OS                OS                    `json:"os,omitempty"`
	Screen            Screen                `json:"screen,omitempty"`
	UserAgent         string                `json:"userAgent,omitempty"`
	Locale            string                `json:"locale,omitempty"`
	Timezone          string                `json:"timezone,omitempty"`
	SessionID         int64                 `json:"sessionId,omitempty"`
	SessionStart      bool                  `json:"sessionStart,omitempty"`
	Traits            map[string]any        `json:"traits,omitempty"`
	ConsentManagement *ConsentManagement    `json:"consentManagement,omitempty"`
	IP                string                `json:"ip,omitempty"`
}

// Library identifies the SDK that produced the event.
type Library struct {
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
}

// Page holds the browser page context at the time the event was fired.
type Page struct {
	URL                    string `json:"url,omitempty"`
	Path                   string `json:"path,omitempty"`
	Referrer               string `json:"referrer,omitempty"`
	Search                 string `json:"search,omitempty"`
	Title                  string `json:"title,omitempty"`
	InitialReferrer        string `json:"initial_referrer,omitempty"`
	InitialReferringDomain string `json:"initial_referring_domain,omitempty"`
}

// OS holds the operating system info from the SDK context.
type OS struct {
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
}

// Screen holds viewport and display metrics from the SDK context.
type Screen struct {
	Width       int `json:"width,omitempty"`
	Height      int `json:"height,omitempty"`
	Density     int `json:"density,omitempty"`
	InnerWidth  int `json:"innerWidth,omitempty"`
	InnerHeight int `json:"innerHeight,omitempty"`
}

// ConsentManagement carries per-event consent state as reported by the SDK.
type ConsentManagement struct {
	DeniedConsentIds   []string `json:"deniedConsentIds,omitempty"`
	AllowedConsentIds  []string `json:"allowedConsentIds,omitempty"`
	Provider           string   `json:"provider,omitempty"`
	ResolutionStrategy string   `json:"resolutionStrategy,omitempty"`
}

// --- Helpers ---

// PagePath returns the page path from context, or empty string if not set.
func (e *Event) PagePath() string {
	return e.Context.Page.Path
}

// HasErrorEventName returns true if the event name ends with "Error" (case-sensitive suffix).
// Matches event names such as "Destination Setup Error", "Webhook Send Error", etc.
func (e *Event) HasErrorEventName() bool {
	return strings.HasSuffix(e.Event, "Error")
}

// EffectiveAnonymousID returns AnonymousID, falling back to UserID when AnonymousID
// is absent (rare in browser channel but handled for robustness).
func (e *Event) EffectiveAnonymousID() string {
	if e.AnonymousID != "" {
		return e.AnonymousID
	}
	return e.UserID
}

// PropertiesMap unmarshals Properties into a string-keyed map. Returns nil on
// empty or unparseable JSON — callers must nil-check.
func (e *Event) PropertiesMap() map[string]any {
	if len(e.Properties) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(e.Properties, &m); err != nil {
		return nil
	}
	return m
}

// TraitsMap unmarshals Traits into a string-keyed map. Returns nil on empty or
// unparseable JSON.
func (e *Event) TraitsMap() map[string]any {
	if len(e.Traits) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(e.Traits, &m); err != nil {
		return nil
	}
	return m
}

// IsConsentBlocked returns true when the event carries a non-empty
// deniedConsentIds list — callers should drop such events per §1 of the design.
func (e *Event) IsConsentBlocked() bool {
	return e.Context.ConsentManagement != nil &&
		len(e.Context.ConsentManagement.DeniedConsentIds) > 0
}

// Batch is the top-level shape posted to /v1/batch by the demo-fire scripts.
type Batch struct {
	Batch    []Event `json:"batch"`
	SentAt   string  `json:"sentAt,omitempty"`
	WriteKey string  `json:"-"` // used for Basic Auth, not part of the body
}

// EventRef is a compact, immutable summary of an Event suitable for retention
// in long-lived in-memory state (e.g. window.UserWindow.LastErrorEvent).
//
// We deliberately do NOT keep a reference to the original Event because the
// window manager design (§3.3) keeps aggregations only — we never retain raw
// payloads. EventRef holds the few fields rules engines need (messageId,
// eventName, a sanitized properties subset) and is safe to copy across
// goroutine boundaries.
type EventRef struct {
	MessageID  string         `json:"messageId"`
	EventName  string         `json:"eventName"`
	EventType  string         `json:"eventType"`
	OccurredAt time.Time      `json:"occurredAt"`
	Properties map[string]any `json:"properties,omitempty"`
}

// NewEventRef builds an EventRef from an Event, copying a small properties
// subset. The full properties payload is deliberately not retained — the
// caller may pass nil keys to skip the properties copy entirely. The returned
// EventRef shares no maps with the source event.
func NewEventRef(e *Event, propertyKeys []string) EventRef {
	if e == nil {
		return EventRef{}
	}
	ref := EventRef{
		MessageID:  e.MessageID,
		EventName:  e.Event,
		EventType:  e.Type,
		OccurredAt: e.OriginalTimestamp,
	}
	if len(propertyKeys) == 0 {
		return ref
	}
	props := e.PropertiesMap()
	if len(props) == 0 {
		return ref
	}
	subset := make(map[string]any, len(propertyKeys))
	for _, k := range propertyKeys {
		if v, ok := props[k]; ok {
			subset[k] = v
		}
	}
	if len(subset) > 0 {
		ref.Properties = subset
	}
	return ref
}
