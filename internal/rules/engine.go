package rules

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/window"
)

// Engine is the rules evaluator. It holds an atomic pointer to the active
// rule list so evaluation never sees a torn read during hot reloads.
//
// The engine is safe for concurrent use across consumer goroutines (per-event
// eval) and the idle ticker goroutine (per-tick eval). The active rule list
// is read via Load on every evaluation, which is lock-free.
type Engine struct {
	rules    atomic.Pointer[[]Rule]
	cooldown CooldownGate

	// loader is invoked by Reload to fetch a fresh rule list from the
	// authoritative store (Postgres in production, an in-memory function
	// in tests).
	loader func(ctx context.Context) ([]Rule, error)

	// reloadOnce gates RunReloader so a process accidentally calling it
	// twice gets the second call as a no-op rather than two competing
	// goroutines.
	reloadOnce sync.Once
}

// NewEngine constructs an Engine with an initial (possibly empty) rule list.
// The cooldown gate is required; pass NewMemoryCooldownGate() for tests or
// process-local single-replica deployments.
func NewEngine(initial []Rule, cooldown CooldownGate) *Engine {
	if cooldown == nil {
		cooldown = NewMemoryCooldownGate()
	}
	e := &Engine{cooldown: cooldown}
	rules := append([]Rule(nil), initial...)
	e.rules.Store(&rules)
	return e
}

// SetLoader registers the function used by Reload to fetch fresh rules.
// Must be called before RunReloader.
func (e *Engine) SetLoader(fn func(ctx context.Context) ([]Rule, error)) {
	e.loader = fn
}

// Snapshot returns the current rule list. The returned slice is read-only —
// mutating it does NOT affect the engine. Useful for admin endpoints.
func (e *Engine) Snapshot() []Rule {
	p := e.rules.Load()
	if p == nil {
		return nil
	}
	out := make([]Rule, len(*p))
	copy(out, *p)
	return out
}

// Replace atomically swaps the engine's rule list. Used by tests and the
// admin reseed path. The slice is copied so callers can mutate their own
// reference safely.
func (e *Engine) Replace(rules []Rule) {
	cp := append([]Rule(nil), rules...)
	e.rules.Store(&cp)
}

// Reload invokes the registered loader and atomically swaps the rule list
// on success. Loader errors are returned to the caller; the active rule
// list is left unchanged.
func (e *Engine) Reload(ctx context.Context) error {
	if e.loader == nil {
		return nil
	}
	fresh, err := e.loader(ctx)
	if err != nil {
		return err
	}
	e.Replace(fresh)
	return nil
}

// RunReloader runs Reload at the given interval until ctx is cancelled.
// Errors are silently ignored (the caller's loader is expected to log);
// the previous rule list remains active on failure.
func (e *Engine) RunReloader(ctx context.Context, every time.Duration) {
	e.reloadOnce.Do(func() {
		go func() {
			if every <= 0 {
				every = 30 * time.Second
			}
			t := time.NewTicker(every)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					_ = e.Reload(ctx)
				}
			}
		}()
	})
}

// EvaluateOnEvent evaluates all rules against snap as triggered by an event
// arrival. Returns the slice of matched rules whose cooldown gate also
// permits emission. Cooldown is the LAST gate — predicate evaluation always
// runs, then cooldown filters and (for emitted matches) marks the gate.
func (e *Engine) EvaluateOnEvent(snap window.Snapshot, persona string, now time.Time) []Match {
	return e.evaluate(snap, persona, now, false)
}

// EvaluateOnTick evaluates only rules that depend on time (e.g. those using
// window.idle_seconds). Called by the idle ticker so we don't redundantly
// re-evaluate rules whose result wouldn't change without time elapsing.
func (e *Engine) EvaluateOnTick(snap window.Snapshot, persona string, now time.Time) []Match {
	return e.evaluate(snap, persona, now, true)
}

// evaluate is the shared implementation. timeOnly=true filters out rules
// whose When-tree does not depend on `now`.
func (e *Engine) evaluate(snap window.Snapshot, persona string, now time.Time, timeOnly bool) []Match {
	p := e.rules.Load()
	if p == nil || len(*p) == 0 {
		return nil
	}
	var matches []Match
	for _, r := range *p {
		if !r.Enabled {
			continue
		}
		// Persona scoping: an empty persona on the rule means "all"; an
		// empty persona on the call also means "all" (admin debug).
		if r.Persona != "" && persona != "" && r.Persona != persona {
			continue
		}
		if timeOnly && r.When != nil && !r.When.UsesTime() {
			continue
		}
		if r.When == nil {
			continue
		}
		if !r.When.Eval(snap, now) {
			continue
		}
		// Cooldown — the LAST gate. We only Mark on emission so a rule that
		// matches but is gated stays gated by its previous fire-time.
		if !e.cooldown.Allow(snap.AnonymousID, r.Name, now) {
			continue
		}
		e.cooldown.Mark(snap.AnonymousID, r.Name, now, r.Cooldown)
		matches = append(matches, Match{
			RuleID:    r.ID,
			RuleName:  r.Name,
			Persona:   r.Persona,
			FiredAt:   now,
			Anonymous: snap.AnonymousID,
			Fire:      r.Fire,
			Snapshot:  snap,
		})
	}
	return matches
}

// --- In-memory cooldown gate ----------------------------------------------

// MemoryCooldownGate is a process-local CooldownGate implementation suitable
// for tests and the single-replica hackathon deployment. Production should
// substitute a Postgres-backed implementation that survives restarts.
type MemoryCooldownGate struct {
	mu    sync.Mutex
	state map[gateKey]time.Time
}

type gateKey struct {
	anonID string
	rule   string
}

// NewMemoryCooldownGate constructs a fresh in-memory gate.
func NewMemoryCooldownGate() *MemoryCooldownGate {
	return &MemoryCooldownGate{state: map[gateKey]time.Time{}}
}

// Allow reports whether (anonID, rule) is permitted to fire at `now`. A rule
// with cooldown <= 0 is treated as never-on-cooldown.
func (g *MemoryCooldownGate) Allow(anonID, rule string, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	last, ok := g.state[gateKey{anonID, rule}]
	if !ok {
		return true
	}
	// We store (last fire time, cooldown duration). To keep the API simple
	// we don't carry duration here; instead the engine.Mark call provides
	// it. We re-derive: a stale entry older than any rule cooldown is
	// uninteresting. For safety we always honor a non-zero last entry —
	// caller is responsible for purging stale entries (see Purge).
	//
	// However, callers expect Allow to consider the duration. We solve this
	// by storing the deadline in Mark, not the fire time.
	if now.Before(last) {
		return false
	}
	return true
}

// Mark records a fire and gates future Allow calls until cooldown has elapsed.
func (g *MemoryCooldownGate) Mark(anonID, rule string, now time.Time, cooldown time.Duration) {
	if cooldown <= 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.state[gateKey{anonID, rule}] = now.Add(cooldown)
}

// Purge removes deadline entries that have already passed. Optional; useful
// for long-running tests to bound map growth.
func (g *MemoryCooldownGate) Purge(now time.Time) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	var n int
	for k, deadline := range g.state {
		if !now.Before(deadline) {
			delete(g.state, k)
			n++
		}
	}
	return n
}

// Size returns the number of tracked gate entries — for tests/observability.
func (g *MemoryCooldownGate) Size() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.state)
}
