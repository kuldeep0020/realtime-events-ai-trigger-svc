package filter_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/consumer"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/event"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/filter"
)

// makeEvent constructs a minimal ProcessedEvent for testing.
func makeEvent(writeKey string, ev *event.Event) consumer.ProcessedEvent {
	if ev == nil {
		ev = &event.Event{
			Type:        "track",
			Channel:     "browser",
			AnonymousID: "anon-test",
			MessageID:   "msg-001",
		}
	}
	return consumer.ProcessedEvent{
		PulsarMessageID: "pulsar-001",
		WriteKey:        writeKey,
		SourceID:        "src-001",
		Event:           ev,
		ReceivedAt:      time.Now(),
	}
}

// jsonRaw builds a json.RawMessage from a Go map.
func jsonRaw(t *testing.T, m map[string]any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("jsonRaw: %v", err)
	}
	return json.RawMessage(b)
}

// getStr reads a string value from a JSON RawMessage for assertions.
func getStr(t *testing.T, raw json.RawMessage, key string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("getStr unmarshal: %v", err)
	}
	v, _ := m[key].(string)
	return v
}

// ---- table-driven tests ----

func TestFilter_DisallowedWriteKey(t *testing.T) {
	f := filter.New(filter.Config{
		AllowedWriteKeys: map[string]bool{"allowed-wk": true},
	})

	in := makeEvent("disallowed-wk", nil)
	_, keep := f.Process(in)
	if keep {
		t.Error("expected event to be dropped for disallowed write key")
	}

	dropped, _, _ := f.Stats()
	if dropped != 1 {
		t.Errorf("expected dropped=1, got %d", dropped)
	}
}

func TestFilter_AllowedWriteKey(t *testing.T) {
	f := filter.New(filter.Config{
		AllowedWriteKeys: map[string]bool{"allowed-wk": true},
	})

	in := makeEvent("allowed-wk", nil)
	_, keep := f.Process(in)
	if !keep {
		t.Error("expected event to pass for allowed write key")
	}
}

func TestFilter_EmptyAllowListPermitsAll(t *testing.T) {
	f := filter.New(filter.Config{
		AllowedWriteKeys: nil, // empty = all allowed
	})

	in := makeEvent("any-write-key", nil)
	_, keep := f.Process(in)
	if !keep {
		t.Error("expected event to pass when AllowedWriteKeys is nil")
	}
}

func TestFilter_DeniedConsentDrop(t *testing.T) {
	f := filter.New(filter.Config{
		DropOnConsentDeny: []string{"consent-id-marketing"},
	})

	ev := &event.Event{
		Type:        "track",
		AnonymousID: "anon-001",
		MessageID:   "msg-consent",
		Context: event.Context{
			ConsentManagement: &event.ConsentManagement{
				DeniedConsentIds: []string{"consent-id-marketing"},
			},
		},
	}
	in := makeEvent("wk-1", ev)
	_, keep := f.Process(in)
	if keep {
		t.Error("expected event to be dropped due to denied consent")
	}

	dropped, _, _ := f.Stats()
	if dropped != 1 {
		t.Errorf("expected dropped=1, got %d", dropped)
	}
}

func TestFilter_DeniedConsentPartialMatch(t *testing.T) {
	// Only "consent-id-analytics" is in the drop list; the event has both
	// "consent-id-marketing" (not in list) and "consent-id-analytics" (in list).
	// The event must be dropped because ANY match triggers a drop.
	f := filter.New(filter.Config{
		DropOnConsentDeny: []string{"consent-id-analytics"},
	})

	ev := &event.Event{
		Type:        "track",
		AnonymousID: "anon-001",
		MessageID:   "msg-partial-consent",
		Context: event.Context{
			ConsentManagement: &event.ConsentManagement{
				DeniedConsentIds: []string{"consent-id-marketing", "consent-id-analytics"},
			},
		},
	}
	in := makeEvent("wk-1", ev)
	_, keep := f.Process(in)
	if keep {
		t.Error("expected drop when any deniedConsentId matches")
	}
}

func TestFilter_ConsentNotInDropList_PassesThrough(t *testing.T) {
	f := filter.New(filter.Config{
		DropOnConsentDeny: []string{"consent-id-marketing"},
	})

	ev := &event.Event{
		Type:        "track",
		AnonymousID: "anon-001",
		MessageID:   "msg-consent-ok",
		Context: event.Context{
			ConsentManagement: &event.ConsentManagement{
				DeniedConsentIds: []string{"consent-id-analytics"}, // not in drop list
			},
		},
	}
	in := makeEvent("wk-1", ev)
	_, keep := f.Process(in)
	if !keep {
		t.Error("expected event to pass when denied consent ID is not in drop list")
	}
}

func TestFilter_RedactPropertiesEmail(t *testing.T) {
	f := filter.New(filter.Config{
		RedactPaths: []string{"properties.email"},
	})

	ev := &event.Event{
		Type:        "track",
		AnonymousID: "anon-001",
		MessageID:   "msg-redact",
		Properties:  jsonRaw(t, map[string]any{"email": "user@example.com", "plan": "free"}),
	}
	in := makeEvent("wk-1", ev)
	out, keep := f.Process(in)
	if !keep {
		t.Fatal("expected event to pass")
	}

	email := getStr(t, out.Event.Properties, "email")
	if email != "[REDACTED]" {
		t.Errorf("expected email to be [REDACTED], got %q", email)
	}
	// Other fields must be untouched.
	plan := getStr(t, out.Event.Properties, "plan")
	if plan != "free" {
		t.Errorf("expected plan to be unchanged 'free', got %q", plan)
	}

	_, redactedCount, _ := f.Stats()
	if redactedCount != 1 {
		t.Errorf("expected redacted=1, got %d", redactedCount)
	}
}

func TestFilter_RedactTraitsPhone(t *testing.T) {
	f := filter.New(filter.Config{
		RedactPaths: []string{"traits.phone"},
	})

	ev := &event.Event{
		Type:        "identify",
		AnonymousID: "anon-001",
		MessageID:   "msg-traits-redact",
		Traits:      jsonRaw(t, map[string]any{"phone": "+1-555-1234", "name": "Alice"}),
	}
	in := makeEvent("wk-1", ev)
	out, keep := f.Process(in)
	if !keep {
		t.Fatal("expected event to pass")
	}

	phone := getStr(t, out.Event.Traits, "phone")
	if phone != "[REDACTED]" {
		t.Errorf("expected phone to be [REDACTED], got %q", phone)
	}
	name := getStr(t, out.Event.Traits, "name")
	if name != "Alice" {
		t.Errorf("expected name to be unchanged 'Alice', got %q", name)
	}
}

func TestFilter_RedactContextTraits(t *testing.T) {
	f := filter.New(filter.Config{
		RedactPaths: []string{"context.traits.email"},
	})

	ev := &event.Event{
		Type:        "track",
		AnonymousID: "anon-001",
		MessageID:   "msg-ctx-traits",
		Context: event.Context{
			Traits: map[string]any{"email": "ctx@example.com", "age": 30},
		},
	}
	in := makeEvent("wk-1", ev)
	out, keep := f.Process(in)
	if !keep {
		t.Fatal("expected event to pass")
	}

	ctxEmail, _ := out.Event.Context.Traits["email"].(string)
	if ctxEmail != "[REDACTED]" {
		t.Errorf("expected context.traits.email to be [REDACTED], got %q", ctxEmail)
	}
	age, ok := out.Event.Context.Traits["age"]
	if !ok {
		t.Error("expected context.traits.age to still exist")
	} else if age != 30 { // original map has int(30)
		t.Errorf("expected context.traits.age unchanged (30), got %v", age)
	}
}

func TestFilter_NoOpPassThrough(t *testing.T) {
	f := filter.New(filter.Config{
		AllowedWriteKeys:  map[string]bool{"wk-1": true},
		RedactPaths:       []string{"properties.email"},
		DropOnConsentDeny: []string{"consent-blocked"},
	})

	// Event with matching write key, no denied consent, and no properties field.
	ev := &event.Event{
		Type:        "page",
		AnonymousID: "anon-clean",
		MessageID:   "msg-clean",
		Properties:  jsonRaw(t, map[string]any{"page": "/home"}),
	}
	in := makeEvent("wk-1", ev)
	out, keep := f.Process(in)
	if !keep {
		t.Fatal("expected event to pass through")
	}
	// No email field — nothing redacted.
	page := getStr(t, out.Event.Properties, "page")
	if page != "/home" {
		t.Errorf("expected page=/home unchanged, got %q", page)
	}
}

func TestFilter_RedactMalformedJSON_PanicSafe(t *testing.T) {
	// Malformed properties JSON must not panic — the redact should be a no-op.
	f := filter.New(filter.Config{
		RedactPaths: []string{"properties.email"},
	})

	ev := &event.Event{
		Type:        "track",
		AnonymousID: "anon-bad",
		MessageID:   "msg-bad-json",
		Properties:  json.RawMessage(`{not valid json`),
	}
	in := makeEvent("wk-1", ev)

	// This must not panic.
	out, keep := f.Process(in)
	if !keep {
		t.Error("malformed JSON should not cause drop — only no-op redact")
	}
	// Properties unchanged (still malformed).
	if string(out.Event.Properties) != `{not valid json` {
		t.Errorf("expected properties unchanged, got %s", string(out.Event.Properties))
	}
}

func TestFilter_RedactNilProperties_PanicSafe(t *testing.T) {
	// Nil properties must not panic.
	f := filter.New(filter.Config{
		RedactPaths: []string{"properties.email", "traits.phone"},
	})

	ev := &event.Event{
		Type:        "track",
		AnonymousID: "anon-nil",
		MessageID:   "msg-nil-props",
		// Properties and Traits are nil
	}
	in := makeEvent("wk-1", ev)

	// Must not panic.
	_, keep := f.Process(in)
	if !keep {
		t.Error("nil properties should not cause drop")
	}
}

func TestFilter_MultipleRedactPaths(t *testing.T) {
	f := filter.New(filter.Config{
		RedactPaths: []string{"properties.email", "properties.phone", "traits.email"},
	})

	ev := &event.Event{
		Type:        "track",
		AnonymousID: "anon-multi",
		MessageID:   "msg-multi",
		Properties:  jsonRaw(t, map[string]any{"email": "a@b.com", "phone": "555", "plan": "pro"}),
		Traits:      jsonRaw(t, map[string]any{"email": "t@b.com", "name": "Bob"}),
	}
	in := makeEvent("wk-1", ev)
	out, keep := f.Process(in)
	if !keep {
		t.Fatal("expected pass")
	}

	if getStr(t, out.Event.Properties, "email") != "[REDACTED]" {
		t.Error("properties.email should be redacted")
	}
	if getStr(t, out.Event.Properties, "phone") != "[REDACTED]" {
		t.Error("properties.phone should be redacted")
	}
	if getStr(t, out.Event.Properties, "plan") != "pro" {
		t.Error("properties.plan should be unchanged")
	}
	if getStr(t, out.Event.Traits, "email") != "[REDACTED]" {
		t.Error("traits.email should be redacted")
	}
	if getStr(t, out.Event.Traits, "name") != "Bob" {
		t.Error("traits.name should be unchanged")
	}
}
