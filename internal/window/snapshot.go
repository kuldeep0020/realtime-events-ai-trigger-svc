// Package window holds in-memory per-user aggregations (§3.3 of the design).
//
// The window manager intentionally retains NO raw event payloads. Aggregations
// are recomputed incrementally as events arrive. Full event history (for the
// LLM enricher) is fetched from Postgres at trigger time.
//
// Concurrency model (§2.3): the store is sharded by fnv32(anonymousId) % N.
// Each shard owns a sync.RWMutex. Mutations occur under the shard's write lock
// via Store.WithWindow (single-writer semantics per anonymousId). Readers
// (Snapshot, ScanIdle) take the shard's read lock and deep-copy the window so
// callers receive a value that is safe to access concurrently after the lock
// is released.
package window

import (
	"time"

	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/event"
)

// Snapshot is an immutable, deep-copied view of a UserWindow at a point in
// time. Returned by Store.Snapshot and emitted by Store.ScanIdle. All map and
// slice fields are owned by the snapshot — callers may read freely without
// holding any lock.
//
// JSON tags use snake_case to match the SSEWindowPayload TypeScript type in
// frontend/types/api.ts. Extra backend fields (PropertyMaxNum, PropertyLast,
// Traits, SessionID, TriggeredRules, FirstSeen, DistinctPaths, PathLatest,
// LastErrorEvent) are serialized but the frontend ignores them.
type Snapshot struct {
	AnonymousID    string             `json:"anonymous_id"`
	UserID         string             `json:"user_id,omitempty"`
	EventCount     int                `json:"event_count"`
	EventTypeCount map[string]int     `json:"event_type_count"`
	EventNameCount map[string]int     `json:"event_name_count"`
	DistinctPaths  map[string]int     `json:"distinct_paths,omitempty"`
	PathLatest     string             `json:"path_latest,omitempty"`
	PropertyMaxNum map[string]float64 `json:"property_max_num,omitempty"`
	PropertyLast   map[string]any     `json:"property_last,omitempty"`
	HasErrorEvent  bool               `json:"has_error_event"`
	LastErrorEvent event.EventRef     `json:"last_error_event,omitempty"`
	FirstSeen      time.Time          `json:"first_seen,omitempty"`
	LastSeen       time.Time          `json:"last_seen"`
	Traits         map[string]any     `json:"traits,omitempty"`
	SessionID      int64              `json:"session_id,omitempty"`
	TriggeredRules map[string]time.Time `json:"triggered_rules,omitempty"`
	// IdleSeconds is the number of whole seconds the window has been idle as
	// of the snapshot. Populated by Store.Snapshot so SSE consumers (and the
	// frontend's TriggerCard "Why" section) can display the idle figure without
	// a separate computation.
	IdleSeconds int `json:"idle_seconds"`
}

// IdleFor returns how long the window has been idle relative to now. Zero or
// negative durations are clamped to 0.
func (s Snapshot) IdleFor(now time.Time) time.Duration {
	if s.LastSeen.IsZero() {
		return 0
	}
	d := now.Sub(s.LastSeen)
	if d < 0 {
		return 0
	}
	return d
}

// copyStringIntMap returns a fresh map with the same contents.
func copyStringIntMap(m map[string]int) map[string]int {
	if len(m) == 0 {
		return map[string]int{}
	}
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func copyStringFloat64Map(m map[string]float64) map[string]float64 {
	if len(m) == 0 {
		return map[string]float64{}
	}
	out := make(map[string]float64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// copyStringAnyMap deep-copies a map[string]any, recursively cloning nested
// maps and slices. Required because Snapshot promises callers ownership of
// all fields — and JSON-decoded properties commonly contain []any (e.g.
// `tech_stack: [...]`) and map[string]any nested values. A shallow copy would
// let snapshot consumers mutate live window state.
func copyStringAnyMap(m map[string]any) map[string]any {
	if len(m) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = deepCopyValue(v)
	}
	return out
}

// deepCopyValue clones a JSON-decoded value (map[string]any, []any, or any
// primitive). Primitives (string, float64, bool, nil, time.Time, etc.) are
// returned by value. Anything else is treated as opaque — same-pointer return.
//
// Bounded recursion: cycles are not possible in JSON-decoded data, so we
// don't track visited pointers.
func deepCopyValue(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case map[string]any:
		return copyStringAnyMap(x)
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = deepCopyValue(item)
		}
		return out
	case []string:
		out := make([]string, len(x))
		copy(out, x)
		return out
	case []float64:
		out := make([]float64, len(x))
		copy(out, x)
		return out
	case []int:
		out := make([]int, len(x))
		copy(out, x)
		return out
	default:
		// Primitive (string, number, bool, time.Time) — safe to share.
		return v
	}
}

func copyStringTimeMap(m map[string]time.Time) map[string]time.Time {
	if len(m) == 0 {
		return map[string]time.Time{}
	}
	out := make(map[string]time.Time, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func copyEventRef(r event.EventRef) event.EventRef {
	if r.Properties == nil {
		return r
	}
	out := r
	out.Properties = copyStringAnyMap(r.Properties)
	return out
}
