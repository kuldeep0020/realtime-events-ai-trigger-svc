// Package filter implements the write-key allow-list, consent enforcement, and
// path-based field redaction described in §3.2 of the design document.
package filter

import (
	"encoding/json"
	"strings"
	"sync/atomic"

	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/consumer"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/event"
)

const redactedValue = "[REDACTED]"

// Config controls which events are kept and what fields are scrubbed.
type Config struct {
	// AllowedWriteKeys is the set of write keys that are permitted through.
	// When empty (nil or zero-length map), ALL write keys are allowed — useful
	// for local dev with a single workspace.
	AllowedWriteKeys map[string]bool

	// RedactPaths lists dotted field paths whose leaf values will be replaced
	// with "[REDACTED]". Only top-level event containers are supported:
	//   properties.<field>   → Event.Properties JSON object
	//   traits.<field>       → Event.Traits JSON object
	//   context.traits.<field> → Event.Context.Traits map
	RedactPaths []string

	// DropOnConsentDeny is the set of consent IDs whose presence in
	// context.consentManagement.deniedConsentIds causes the event to be dropped.
	// An event is dropped when ANY of its deniedConsentIds is in this set.
	DropOnConsentDeny []string
}

// Filter applies write-key filtering, consent enforcement, and field redaction.
type Filter struct {
	cfg Config

	// deniedSet is the pre-built lookup set for DropOnConsentDeny.
	deniedSet map[string]bool

	// counters
	dropped  atomic.Int64
	redacted atomic.Int64
	passed   atomic.Int64
}

// New creates a Filter from the given Config.
func New(cfg Config) *Filter {
	deniedSet := make(map[string]bool, len(cfg.DropOnConsentDeny))
	for _, id := range cfg.DropOnConsentDeny {
		deniedSet[id] = true
	}
	return &Filter{cfg: cfg, deniedSet: deniedSet}
}

// Process applies the filter pipeline to a single ProcessedEvent. It returns
// the (potentially mutated) event and keep=true when the event should proceed,
// or keep=false when it should be dropped.
//
// Mutations are applied to a shallow copy of the event's JSON fields; the
// original ProcessedEvent value is not modified in place.
func (f *Filter) Process(in consumer.ProcessedEvent) (out consumer.ProcessedEvent, keep bool) {
	// 1. Write-key allow-list.
	if len(f.cfg.AllowedWriteKeys) > 0 {
		if !f.cfg.AllowedWriteKeys[in.WriteKey] {
			f.dropped.Add(1)
			return in, false
		}
	}

	// 2. Consent enforcement.
	if f.isConsentDenied(in.Event) {
		f.dropped.Add(1)
		return in, false
	}

	// 3. Field redaction.
	if len(f.cfg.RedactPaths) > 0 && in.Event != nil {
		ev := *in.Event // shallow copy of Event struct
		redacted := f.redactEvent(&ev)
		out = in
		out.Event = &ev
		if redacted {
			f.redacted.Add(1)
		}
		f.passed.Add(1)
		return out, true
	}

	f.passed.Add(1)
	return in, true
}

// isConsentDenied returns true when any deniedConsentId in the event matches
// the configured drop-list.
func (f *Filter) isConsentDenied(ev *event.Event) bool {
	if ev == nil {
		return false
	}
	if len(f.deniedSet) == 0 {
		return false
	}
	cm := ev.Context.ConsentManagement
	if cm == nil {
		return false
	}
	for _, id := range cm.DeniedConsentIds {
		if f.deniedSet[id] {
			return true
		}
	}
	return false
}

// redactEvent applies path redactions to ev in-place (ev is already a copy).
// Returns true if at least one field was redacted.
func (f *Filter) redactEvent(ev *event.Event) bool {
	anyRedacted := false

	for _, path := range f.cfg.RedactPaths {
		parts := strings.SplitN(path, ".", 3)
		if len(parts) < 2 {
			continue
		}

		top := parts[0]
		switch top {
		case "properties":
			if len(parts) == 2 {
				if redactRawField(&ev.Properties, parts[1]) {
					anyRedacted = true
				}
			}
		case "traits":
			if len(parts) == 2 {
				if redactRawField(&ev.Traits, parts[1]) {
					anyRedacted = true
				}
			}
		case "context":
			if len(parts) >= 3 && parts[1] == "traits" {
				fieldKey := parts[2]
				if redactContextTraits(&ev.Context.Traits, fieldKey) {
					anyRedacted = true
				}
			}
		}
	}

	return anyRedacted
}

// redactRawField unmarshals a JSON object, sets the target key to
// "[REDACTED]", and re-marshals it back. If the path does not exist or the
// JSON is malformed, the function is a no-op and returns false.
// Panics are explicitly avoided: nil/empty inputs return false immediately.
func redactRawField(raw *json.RawMessage, key string) bool {
	if raw == nil || len(*raw) == 0 {
		return false
	}

	var m map[string]any
	if err := json.Unmarshal(*raw, &m); err != nil {
		// Not a JSON object (e.g., null, array) — skip silently.
		return false
	}

	if _, exists := m[key]; !exists {
		return false
	}

	m[key] = redactedValue

	b, err := json.Marshal(m)
	if err != nil {
		// This should never happen for map[string]any — skip silently.
		return false
	}

	*raw = json.RawMessage(b)
	return true
}

// redactContextTraits sets a single key in the Context.Traits map to
// "[REDACTED]". If the map is nil or the key absent, it is a no-op.
func redactContextTraits(traits *map[string]any, key string) bool {
	if traits == nil || *traits == nil {
		return false
	}
	if _, exists := (*traits)[key]; !exists {
		return false
	}
	(*traits)[key] = redactedValue
	return true
}

// Stats returns a snapshot of the filter counters.
func (f *Filter) Stats() (dropped, redacted, passed int64) {
	return f.dropped.Load(), f.redacted.Load(), f.passed.Load()
}
