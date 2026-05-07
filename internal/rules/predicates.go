package rules

import (
	"fmt"
	"strings"
	"time"

	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/window"
)

// predicateSpec describes a registered predicate: its build (parse-time)
// function and whether it consumes time (`now`).
type predicateSpec struct {
	build    func(args map[string]any) (predicateFn, error)
	usesTime bool
}

// registry is read-only after init(). New predicates added at runtime would
// require a sync layer — we don't, by design.
var registry = map[string]predicateSpec{}

func init() {
	registry["window.event_count"] = predicateSpec{build: buildEventCount}
	registry["window.event_count_of_type"] = predicateSpec{build: buildEventCountOfType}
	registry["window.event_count_of_name"] = predicateSpec{build: buildEventCountOfName}
	registry["window.idle_seconds"] = predicateSpec{build: buildIdleSeconds, usesTime: true}
	registry["window.has_event_type"] = predicateSpec{build: buildHasEventType}
	registry["window.has_event_name"] = predicateSpec{build: buildHasEventName}
	registry["window.event_path_matches"] = predicateSpec{build: buildEventPathMatches}
	registry["window.has_property"] = predicateSpec{build: buildHasProperty}
	registry["window.property_value"] = predicateSpec{build: buildPropertyValue}
	registry["window.distinct_paths_at_least"] = predicateSpec{build: buildDistinctPathsAtLeast}
	registry["window.has_error_event"] = predicateSpec{build: buildHasErrorEvent}
	registry["window.session_event_count"] = predicateSpec{build: buildSessionEventCount}
	registry["traits.known"] = predicateSpec{build: buildTraitsKnown}
	registry["traits.value"] = predicateSpec{build: buildTraitsValue}
}

// --- Operator helper -------------------------------------------------------

// applyOp evaluates `lhs op rhs`. Numeric operators coerce ints/floats; `in`
// expects rhs to be a slice ([]any); `matches` requires rhs to be a string.
//
// Returns false for unrecognized operators or incompatible types — predicates
// are expected to be conservative (false rather than crash).
func applyOp(op string, lhs, rhs any) bool {
	switch op {
	case "==", "eq":
		return equalAny(lhs, rhs)
	case "!=", "ne":
		return !equalAny(lhs, rhs)
	case ">", "gt":
		l, r, ok := numerics(lhs, rhs)
		return ok && l > r
	case ">=", "gte":
		l, r, ok := numerics(lhs, rhs)
		return ok && l >= r
	case "<", "lt":
		l, r, ok := numerics(lhs, rhs)
		return ok && l < r
	case "<=", "lte":
		l, r, ok := numerics(lhs, rhs)
		return ok && l <= r
	case "in":
		// rhs is the membership set.
		arr, ok := toAnySlice(rhs)
		if !ok {
			return false
		}
		for _, v := range arr {
			if equalAny(lhs, v) {
				return true
			}
		}
		return false
	case "matches":
		s, ok := lhs.(string)
		if !ok {
			return false
		}
		pattern, ok := rhs.(string)
		if !ok {
			return false
		}
		r, err := compileRegex(pattern)
		if err != nil {
			return false
		}
		return r.MatchString(s)
	}
	return false
}

// equalAny compares two values, coercing numeric types so `int(3) == float64(3)`.
func equalAny(a, b any) bool {
	if a == nil || b == nil {
		return a == b
	}
	if l, r, ok := numerics(a, b); ok {
		return l == r
	}
	if as, ok := a.(string); ok {
		if bs, ok := b.(string); ok {
			return as == bs
		}
	}
	if ab, ok := a.(bool); ok {
		if bb, ok := b.(bool); ok {
			return ab == bb
		}
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}

// numerics coerces both operands to float64 if both are numeric. JSON
// unmarshalling produces float64 by default; we additionally accept int /
// int64 from rule specs hand-built in Go.
func numerics(a, b any) (float64, float64, bool) {
	la, oka := toFloat(a)
	lb, okb := toFloat(b)
	if !oka || !okb {
		return 0, 0, false
	}
	return la, lb, true
}

func toFloat(v any) (float64, bool) {
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
	case uint:
		return float64(n), true
	case uint64:
		return float64(n), true
	default:
		return 0, false
	}
}

func toAnySlice(v any) ([]any, bool) {
	switch s := v.(type) {
	case []any:
		return s, true
	case []string:
		out := make([]any, len(s))
		for i, x := range s {
			out[i] = x
		}
		return out, true
	case []float64:
		out := make([]any, len(s))
		for i, x := range s {
			out[i] = x
		}
		return out, true
	case []int:
		out := make([]any, len(s))
		for i, x := range s {
			out[i] = x
		}
		return out, true
	}
	return nil, false
}

// extractOpValue parses a comparison spec from args. Two supported shapes:
//
//	{ "op": "...", "value": ... }            // explicit
//	{ ">=": 3 } / { "==": "foo" } / etc.    // inline (used by §5 configs)
//
// Returns op, value, ok.
func extractOpValue(args map[string]any) (string, any, bool) {
	if op, ok := args["op"].(string); ok {
		return op, args["value"], true
	}
	for _, o := range []string{"==", "!=", ">", ">=", "<", "<=", "in", "matches", "eq", "ne", "gt", "gte", "lt", "lte"} {
		if v, ok := args[o]; ok {
			return o, v, true
		}
	}
	return "", nil, false
}

// requireString returns the string at args[key] or an error.
func requireString(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", fmt.Errorf("missing required arg %q", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("arg %q must be a string, got %T", key, v)
	}
	return s, nil
}

// --- Builders --------------------------------------------------------------

func buildEventCount(args map[string]any) (predicateFn, error) {
	op, val, ok := extractOpValue(args)
	if !ok {
		return nil, fmt.Errorf("window.event_count: missing op/value")
	}
	return func(snap window.Snapshot, _ time.Time, _ *Predicate) bool {
		return applyOp(op, snap.EventCount, val)
	}, nil
}

func buildEventCountOfType(args map[string]any) (predicateFn, error) {
	typ, err := requireString(args, "type")
	if err != nil {
		return nil, err
	}
	op, val, ok := extractOpValue(args)
	if !ok {
		return nil, fmt.Errorf("window.event_count_of_type: missing op/value")
	}
	return func(snap window.Snapshot, _ time.Time, _ *Predicate) bool {
		return applyOp(op, snap.EventTypeCount[typ], val)
	}, nil
}

func buildEventCountOfName(args map[string]any) (predicateFn, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return nil, err
	}
	op, val, ok := extractOpValue(args)
	if !ok {
		return nil, fmt.Errorf("window.event_count_of_name: missing op/value")
	}
	return func(snap window.Snapshot, _ time.Time, _ *Predicate) bool {
		return applyOp(op, snap.EventNameCount[name], val)
	}, nil
}

func buildIdleSeconds(args map[string]any) (predicateFn, error) {
	op, val, ok := extractOpValue(args)
	if !ok {
		return nil, fmt.Errorf("window.idle_seconds: missing op/value")
	}
	return func(snap window.Snapshot, now time.Time, _ *Predicate) bool {
		idle := snap.IdleFor(now).Seconds()
		return applyOp(op, idle, val)
	}, nil
}

func buildHasEventType(args map[string]any) (predicateFn, error) {
	typ, err := stringFromShorthand(args)
	if err != nil {
		return nil, fmt.Errorf("window.has_event_type: %w", err)
	}
	return func(snap window.Snapshot, _ time.Time, _ *Predicate) bool {
		return snap.EventTypeCount[typ] > 0
	}, nil
}

func buildHasEventName(args map[string]any) (predicateFn, error) {
	name, err := stringFromShorthand(args)
	if err != nil {
		return nil, fmt.Errorf("window.has_event_name: %w", err)
	}
	return func(snap window.Snapshot, _ time.Time, _ *Predicate) bool {
		return snap.EventNameCount[name] > 0
	}, nil
}

func buildEventPathMatches(args map[string]any) (predicateFn, error) {
	pattern, err := stringFromShorthand(args)
	if err != nil {
		return nil, fmt.Errorf("window.event_path_matches: %w", err)
	}
	r, err := compileRegex(pattern)
	if err != nil {
		return nil, err
	}
	return func(snap window.Snapshot, _ time.Time, _ *Predicate) bool {
		// Match against any visited path; PathLatest alone is too narrow.
		// The §5.1 rule uses this to scope to listing pages.
		for path := range snap.DistinctPaths {
			if r.MatchString(path) {
				return true
			}
		}
		return false
	}, nil
}

func buildHasProperty(args map[string]any) (predicateFn, error) {
	path, err := stringFromShorthand(args)
	if err != nil {
		// Also accept {"path": "..."} form
		if p, perr := requireString(args, "path"); perr == nil {
			path = p
		} else {
			return nil, fmt.Errorf("window.has_property: %w", err)
		}
	}
	return func(snap window.Snapshot, _ time.Time, _ *Predicate) bool {
		_, ok := snap.PropertyLast[path]
		return ok
	}, nil
}

func buildPropertyValue(args map[string]any) (predicateFn, error) {
	path, err := requireString(args, "path")
	if err != nil {
		return nil, err
	}
	op, val, ok := extractOpValue(args)
	if !ok {
		return nil, fmt.Errorf("window.property_value: missing op/value")
	}
	return func(snap window.Snapshot, _ time.Time, _ *Predicate) bool {
		v, ok := snap.PropertyLast[path]
		if !ok {
			return false
		}
		return applyOp(op, v, val)
	}, nil
}

func buildDistinctPathsAtLeast(args map[string]any) (predicateFn, error) {
	// Accept either an inline number or a {"value": N} form.
	var threshold float64
	if v, ok := args["value"]; ok {
		f, ok := toFloat(v)
		if !ok {
			return nil, fmt.Errorf("window.distinct_paths_at_least: value must be numeric")
		}
		threshold = f
	} else {
		// shorthand: {"window.distinct_paths_at_least": 3}
		if v, ok := shorthandValue(args); ok {
			f, ok := toFloat(v)
			if !ok {
				return nil, fmt.Errorf("window.distinct_paths_at_least: shorthand must be numeric")
			}
			threshold = f
		} else {
			return nil, fmt.Errorf("window.distinct_paths_at_least: missing value")
		}
	}
	return func(snap window.Snapshot, _ time.Time, _ *Predicate) bool {
		return float64(len(snap.DistinctPaths)) >= threshold
	}, nil
}

func buildHasErrorEvent(_ map[string]any) (predicateFn, error) {
	return func(snap window.Snapshot, _ time.Time, _ *Predicate) bool {
		return snap.HasErrorEvent
	}, nil
}

func buildSessionEventCount(args map[string]any) (predicateFn, error) {
	op, val, ok := extractOpValue(args)
	if !ok {
		return nil, fmt.Errorf("window.session_event_count: missing op/value")
	}
	// We don't currently track per-session event count distinctly from
	// EventCount; for the hackathon scope a session is the window. If a new
	// session id arrives, a new window is the assumption. We expose this
	// predicate so future sessionization is a non-breaking change.
	return func(snap window.Snapshot, _ time.Time, _ *Predicate) bool {
		return applyOp(op, snap.EventCount, val)
	}, nil
}

func buildTraitsKnown(args map[string]any) (predicateFn, error) {
	path, err := stringFromShorthand(args)
	if err != nil {
		if p, perr := requireString(args, "path"); perr == nil {
			path = p
		} else {
			return nil, fmt.Errorf("traits.known: %w", err)
		}
	}
	return func(snap window.Snapshot, _ time.Time, _ *Predicate) bool {
		_, ok := snap.Traits[path]
		return ok
	}, nil
}

func buildTraitsValue(args map[string]any) (predicateFn, error) {
	path, err := requireString(args, "path")
	if err != nil {
		return nil, err
	}
	op, val, ok := extractOpValue(args)
	if !ok {
		return nil, fmt.Errorf("traits.value: missing op/value")
	}
	return func(snap window.Snapshot, _ time.Time, _ *Predicate) bool {
		v, ok := snap.Traits[path]
		if !ok {
			return false
		}
		return applyOp(op, v, val)
	}, nil
}

// --- Shorthand helpers -----------------------------------------------------

// stringFromShorthand pulls a single string value from a shorthand args map
// (one with a single non-operator key, used in §5 configs like
// `window.has_event_type: page` which becomes args = {"value": "page"} after
// our normalization).
func stringFromShorthand(args map[string]any) (string, error) {
	if v, ok := shorthandValue(args); ok {
		s, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("expected string, got %T", v)
		}
		return s, nil
	}
	return "", fmt.Errorf("missing value")
}

// shorthandValue returns the args["value"] entry if present (this is where
// our spec normalizer puts unwrapped scalars).
func shorthandValue(args map[string]any) (any, bool) {
	v, ok := args["value"]
	return v, ok
}

// --- Public introspection --------------------------------------------------

// RegisteredPredicates returns the sorted list of predicate names. Used by
// tests and the admin debug endpoint.
func RegisteredPredicates() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	// Stable order — sort would import sort; small list, simple insertion sort
	// inline to keep this file's imports tight.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && strings.Compare(out[j-1], out[j]) > 0; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
