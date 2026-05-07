package window

import (
	"strings"
	"time"

	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/event"
)

// errorPropertyKeys is the small subset of property fields we capture into
// LastErrorEvent. Keeping this list short avoids retaining large payloads.
var errorPropertyKeys = []string{
	"error_code",
	"error_message",
	"destination_type",
	"source_type",
	"step",
	"elapsed_seconds_in_step",
}

// propertyTrackKeys are the property paths PropertyMaxNum / PropertyLast
// observe. We intentionally limit to a small set known to be useful for rules
// (price, bedrooms, sq_ft, results_count, elapsed_seconds_in_step). Adding
// new keys is O(events) so we keep the list tight.
//
// PropertyMaxNum tracks the max numeric value seen; PropertyLast tracks the
// most recent value (numeric or otherwise) for the same keys.
var propertyTrackKeys = []string{
	"price",
	"bedrooms",
	"sq_ft",
	"results_count",
	"elapsed_seconds_in_step",
	"elapsed_seconds_in_setup",
	"setup_minutes_so_far",
	"view_count",
	"listed_days_ago",
	"beds_min",
	"price_min",
	"price_max",
}

// UserWindow holds incrementally-maintained aggregations for a single
// anonymousId. It is NOT goroutine-safe on its own — callers must mutate it
// only while holding the shard write lock (see Store.WithWindow).
type UserWindow struct {
	AnonymousID    string
	UserID         string
	EventCount     int
	EventTypeCount map[string]int     // "page", "track", "identify"
	EventNameCount map[string]int     // event name -> count
	DistinctPaths  map[string]int     // page path -> count
	PathLatest     string             // most recent page.path observed
	PropertyMaxNum map[string]float64 // property key -> max numeric seen
	PropertyLast   map[string]any     // property key -> last value seen
	HasErrorEvent  bool
	LastErrorEvent event.EventRef
	FirstSeen      time.Time
	LastSeen       time.Time
	Traits         map[string]any
	SessionID      int64
	// triggeredRules mirrors DB cooldown state for fast local checks. The
	// rules engine is the source of truth; this is a hint for optimization.
	triggeredRules map[string]time.Time
	// lastTouched tracks when the window was last mutated; used by the LRU
	// eviction policy when the store hits its max-window cap.
	lastTouched time.Time
}

// newUserWindow creates a fresh UserWindow keyed by anonID.
//
// FirstSeen / LastSeen are intentionally left at zero; they are stamped from
// the first event's OriginalTimestamp inside apply. lastTouched is set to the
// creation time so eviction has a deterministic ordering even before any
// event has been applied (an unlikely but possible race).
func newUserWindow(anonID string, now time.Time) *UserWindow {
	return &UserWindow{
		AnonymousID:    anonID,
		EventTypeCount: map[string]int{},
		EventNameCount: map[string]int{},
		DistinctPaths:  map[string]int{},
		PropertyMaxNum: map[string]float64{},
		PropertyLast:   map[string]any{},
		Traits:         map[string]any{},
		triggeredRules: map[string]time.Time{},
		lastTouched:    now,
	}
}

// apply incrementally folds a single event into the window.
//
// receivedAt is the server-side wall-clock time the event was dequeued — used
// as the authoritative clock for idle detection (LastSeen / FirstSeen). Client
// clocks can be skewed or stale (see Bug 1 fix: re-stamping on send still
// leaves demo events bunched within microseconds at script-build time without
// this defence). Using receivedAt means the idle ticker's real_now-LastSeen
// correctly tracks wall-clock silence, not event timestamp skew.
//
// The function is intentionally small and deterministic — it touches only
// fields documented in §3.3 and never panics on malformed payloads.
func (w *UserWindow) apply(e *event.Event, receivedAt time.Time) {
	if e == nil {
		return
	}
	// Use receivedAt as the authoritative time for window bookkeeping.
	// Fall back to e.OriginalTimestamp (then time.Now) if the caller supplied
	// a zero value — this preserves correct behaviour in unit tests that
	// simulate logical time via OriginalTimestamp without a real ReceivedAt.
	now := receivedAt
	if now.IsZero() {
		now = e.OriginalTimestamp
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	w.EventCount++
	if e.Type != "" {
		w.EventTypeCount[e.Type]++
	}
	if e.Event != "" {
		w.EventNameCount[e.Event]++
	}
	if e.UserID != "" && w.UserID == "" {
		w.UserID = e.UserID
	}
	if e.Context.SessionID != 0 {
		w.SessionID = e.Context.SessionID
	}
	if path := e.PagePath(); path != "" {
		w.DistinctPaths[path]++
		w.PathLatest = path
	}

	// Properties — only inspect known keys to bound work.
	if props := e.PropertiesMap(); len(props) > 0 {
		for _, k := range propertyTrackKeys {
			v, ok := props[k]
			if !ok {
				continue
			}
			w.PropertyLast[k] = v
			if f, ok := numericValue(v); ok {
				if cur, exists := w.PropertyMaxNum[k]; !exists || f > cur {
					w.PropertyMaxNum[k] = f
				}
			}
		}
	}

	// Traits — flatten identify-time and context traits into a single map.
	if e.Type == "identify" {
		if traits := e.TraitsMap(); len(traits) > 0 {
			for k, v := range traits {
				w.Traits[k] = v
			}
		}
	}
	if len(e.Context.Traits) > 0 {
		for k, v := range e.Context.Traits {
			// Don't clobber identify-set traits with weaker context ones.
			if _, exists := w.Traits[k]; !exists {
				w.Traits[k] = v
			}
		}
	}

	// Error tracking — names ending in "Error" (matches §6.3).
	if e.HasErrorEventName() {
		w.HasErrorEvent = true
		w.LastErrorEvent = event.NewEventRef(e, errorPropertyKeys)
	}

	if w.FirstSeen.IsZero() || now.Before(w.FirstSeen) {
		w.FirstSeen = now
	}
	if w.LastSeen.IsZero() || now.After(w.LastSeen) {
		w.LastSeen = now
	}
	w.lastTouched = time.Now().UTC()
}

// snapshot returns a deep-copied immutable view. Caller must hold at least
// the shard read lock.
func (w *UserWindow) snapshot() Snapshot {
	return Snapshot{
		AnonymousID:    w.AnonymousID,
		UserID:         w.UserID,
		EventCount:     w.EventCount,
		EventTypeCount: copyStringIntMap(w.EventTypeCount),
		EventNameCount: copyStringIntMap(w.EventNameCount),
		DistinctPaths:  copyStringIntMap(w.DistinctPaths),
		PathLatest:     w.PathLatest,
		PropertyMaxNum: copyStringFloat64Map(w.PropertyMaxNum),
		PropertyLast:   copyStringAnyMap(w.PropertyLast),
		HasErrorEvent:  w.HasErrorEvent,
		LastErrorEvent: copyEventRef(w.LastErrorEvent),
		FirstSeen:      w.FirstSeen,
		LastSeen:       w.LastSeen,
		Traits:         copyStringAnyMap(w.Traits),
		SessionID:      w.SessionID,
		TriggeredRules: copyStringTimeMap(w.triggeredRules),
	}
}

// MarkTriggered records a local cooldown hint. The rules engine writes
// authoritative cooldowns to Postgres; this is purely an in-memory mirror so
// hot-path eval can short-circuit without a DB round-trip.
func (w *UserWindow) MarkTriggered(ruleName string, when time.Time) {
	if ruleName == "" {
		return
	}
	w.triggeredRules[ruleName] = when
}

// CooldownActive returns true if the rule fired within the past `cooldown`
// duration relative to `now`.
func (w *UserWindow) CooldownActive(ruleName string, cooldown time.Duration, now time.Time) bool {
	if cooldown <= 0 {
		return false
	}
	last, ok := w.triggeredRules[ruleName]
	if !ok {
		return false
	}
	return now.Sub(last) < cooldown
}

// numericValue coerces JSON-decoded primitives into float64. Returns false if
// the value is not numeric. JSON unmarshals all numbers to float64 by default;
// we additionally accept int / int64 for callers who hand-build properties.
func numericValue(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case string:
		// Don't auto-parse; rules engine handles string ops.
		_ = n
		return 0, false
	default:
		return 0, false
	}
}

// hasErrorSuffix is exposed for tests; mirrors event.Event.HasErrorEventName
// without requiring an Event allocation.
func hasErrorSuffix(name string) bool {
	return strings.HasSuffix(name, "Error")
}
