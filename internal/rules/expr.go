package rules

import (
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/window"
)

// Expr is the interface every node in the parsed When-tree implements.
//
// EvalContext: a snapshot is the source of truth for predicates; `now` is
// supplied by the evaluator so time-based predicates (idle_seconds) are
// deterministic in tests.
type Expr interface {
	Eval(snap window.Snapshot, now time.Time) bool
	// UsesTime reports whether the expression depends on `now` (i.e. would
	// produce a different result given the same snapshot at a different
	// time). Used by the engine to short-circuit OnTick-only evaluations.
	UsesTime() bool
}

// AllOf is the conjunction of child expressions (logical AND). Empty AllOf
// evaluates to true (vacuously satisfied).
type AllOf struct {
	Children []Expr
}

func (a AllOf) Eval(snap window.Snapshot, now time.Time) bool {
	for _, c := range a.Children {
		if !c.Eval(snap, now) {
			return false
		}
	}
	return true
}

func (a AllOf) UsesTime() bool {
	for _, c := range a.Children {
		if c.UsesTime() {
			return true
		}
	}
	return false
}

// AnyOf is the disjunction of child expressions (logical OR). Empty AnyOf
// evaluates to false.
type AnyOf struct {
	Children []Expr
}

func (a AnyOf) Eval(snap window.Snapshot, now time.Time) bool {
	for _, c := range a.Children {
		if c.Eval(snap, now) {
			return true
		}
	}
	return false
}

func (a AnyOf) UsesTime() bool {
	for _, c := range a.Children {
		if c.UsesTime() {
			return true
		}
	}
	return false
}

// Not negates a child expression.
type Not struct {
	Child Expr
}

func (n Not) Eval(snap window.Snapshot, now time.Time) bool {
	if n.Child == nil {
		return false
	}
	return !n.Child.Eval(snap, now)
}

func (n Not) UsesTime() bool {
	if n.Child == nil {
		return false
	}
	return n.Child.UsesTime()
}

// Predicate is a leaf expression resolved at eval time against the
// predicate registry.
//
// Args is the parsed argument map (keys depend on the predicate). Validation
// (e.g. required args, regex compilation) happens at parse time, not eval
// time.
type Predicate struct {
	Name string
	Args map[string]any
	// fn is bound at parse time (UnmarshalSpec) — eval is therefore a
	// straight call without registry lookup.
	fn       predicateFn
	usesTime bool
	// regexCache holds a pre-compiled regex for predicates that need one.
	// Compiling once at parse time both avoids per-event allocation and
	// makes invalid regexes a load-time error rather than a runtime panic.
	regexCache *regexp.Regexp
}

// predicateFn is the runtime evaluator for a registered predicate.
type predicateFn func(snap window.Snapshot, now time.Time, p *Predicate) bool

func (p *Predicate) Eval(snap window.Snapshot, now time.Time) bool {
	if p.fn == nil {
		return false
	}
	return p.fn(snap, now, p)
}

func (p *Predicate) UsesTime() bool { return p.usesTime }

// regexCacheLock guards regexCachePool — see compileRegex below.
var regexCacheLock sync.RWMutex

// regexCachePool de-duplicates compiled regexes across rules. This is shared
// process-wide; rules referring to the same pattern share a single
// *regexp.Regexp. Bounded growth: distinct patterns in a deployed config
// are O(rules) so unbounded growth isn't a concern.
var regexCachePool = map[string]*regexp.Regexp{}

// compileRegex returns a compiled regex from the shared pool, compiling
// (and caching) on first use. Returns an error for invalid patterns.
func compileRegex(pattern string) (*regexp.Regexp, error) {
	regexCacheLock.RLock()
	if r, ok := regexCachePool[pattern]; ok {
		regexCacheLock.RUnlock()
		return r, nil
	}
	regexCacheLock.RUnlock()

	r, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("compile regex %q: %w", pattern, err)
	}
	regexCacheLock.Lock()
	// Re-check; another goroutine may have inserted it.
	if existing, ok := regexCachePool[pattern]; ok {
		regexCacheLock.Unlock()
		return existing, nil
	}
	regexCachePool[pattern] = r
	regexCacheLock.Unlock()
	return r, nil
}
