package window

import (
	"context"
	"hash/fnv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/event"
)

// shardCount is fixed at 16 (§3.3). Power-of-two enables a fast mask, but FNV
// is well-distributed enough that a modulo is fine.
const shardCount = 16

// DefaultMaxWindows is the default LRU cap when New is called with maxWindows
// <= 0. 5,000 windows comfortably fit in memory while still bounding growth
// during runaway traffic (see §3.3 rationale).
const DefaultMaxWindows = 5_000

// Store is the sharded, in-memory window manager. Construct with New.
type Store struct {
	shards     [shardCount]shard
	maxWindows int

	// active is an atomic counter mirroring the sum of len(shard.windows).
	// It is incremented under shard write lock when adding a new window and
	// decremented when one is evicted/pruned. Snapshot reads of this counter
	// are best-effort (no consistency guarantees vs concurrent mutations),
	// which is fine for metrics and capacity checks.
	active atomic.Int64

	// prunedCh receives anonymousIds for windows the pruner removed. Buffered
	// so a slow consumer doesn't stall the pruner; if full, drops are
	// silently coalesced (this is a best-effort UI hint).
	prunedCh chan string
}

type shard struct {
	mu      sync.RWMutex
	windows map[string]*UserWindow
}

// New constructs a Store with the given LRU cap. Passing maxWindows <= 0 uses
// DefaultMaxWindows. The returned store is ready to use; call RunPruner in a
// separate goroutine if idle expiry is desired.
func New(maxWindows int) *Store {
	if maxWindows <= 0 {
		maxWindows = DefaultMaxWindows
	}
	s := &Store{
		maxWindows: maxWindows,
		prunedCh:   make(chan string, 256),
	}
	for i := range s.shards {
		s.shards[i].windows = make(map[string]*UserWindow)
	}
	return s
}

// shardFor returns the shard owning anonID. fnv32 is fast and well-distributed
// enough that we don't need murmur or xxhash here.
func (s *Store) shardFor(anonID string) *shard {
	h := fnv.New32a()
	_, _ = h.Write([]byte(anonID))
	return &s.shards[h.Sum32()%shardCount]
}

// WithWindow runs fn under the write lock for anonID's shard. The window is
// created on first access. fn must not retain *UserWindow after returning.
//
// Single-writer-per-shard semantics: while fn runs, no other writer for the
// same anonymousId (or any anonymousId mapped to the same shard) can mutate.
// Readers (Snapshot, ScanIdle) are blocked for the duration of fn — fn should
// be short and non-blocking.
func (s *Store) WithWindow(anonID string, fn func(*UserWindow)) {
	if anonID == "" || fn == nil {
		return
	}
	sh := s.shardFor(anonID)

	// Quick-path check: if the window already exists, take the write lock
	// and run fn — no eviction needed.
	sh.mu.Lock()
	if w, ok := sh.windows[anonID]; ok {
		fn(w)
		sh.mu.Unlock()
		return
	}
	sh.mu.Unlock()

	// New-window path: enforce the global LRU cap before insertion. Eviction
	// runs WITHOUT holding the target shard's lock to avoid lock-ordering
	// hazards. The cap is soft — a small overshoot under heavy concurrent
	// insertion is acceptable.
	for int(s.active.Load()) >= s.maxWindows {
		if !s.evictGlobalOldest() {
			break // nothing to evict (all shards empty); fail open
		}
	}

	sh.mu.Lock()
	// Re-check after lock acquisition — another goroutine may have created
	// the window while we were evicting.
	w, ok := sh.windows[anonID]
	if !ok {
		w = newUserWindow(anonID, time.Now().UTC())
		sh.windows[anonID] = w
		s.active.Add(1)
	}
	fn(w)
	sh.mu.Unlock()
}

// Update is a convenience wrapper around WithWindow that applies a single
// event's aggregations. receivedAt is the server-side wall-clock time the
// event arrived; it is used as the authoritative time for FirstSeen/LastSeen
// so idle detection is based on real wall-clock silence, not client timestamps.
// Pass time.Time{} to fall back to time.Now() inside apply.
func (s *Store) Update(evt *event.Event, receivedAt time.Time) {
	if evt == nil {
		return
	}
	anonID := evt.EffectiveAnonymousID()
	if anonID == "" {
		return
	}
	s.WithWindow(anonID, func(w *UserWindow) {
		w.apply(evt, receivedAt)
	})
}

// Snapshot returns an immutable deep-copy of the window for anonID, or
// (zero, false) if no window exists. Safe to call concurrently with WithWindow
// and other reads.
//
// IdleSeconds is populated here using the snapshot's own LastSeen so
// SSE consumers receive a ready-to-display figure without a separate call.
func (s *Store) Snapshot(anonID string) (Snapshot, bool) {
	if anonID == "" {
		return Snapshot{}, false
	}
	sh := s.shardFor(anonID)
	sh.mu.RLock()
	w, ok := sh.windows[anonID]
	if !ok {
		sh.mu.RUnlock()
		return Snapshot{}, false
	}
	snap := w.snapshot()
	sh.mu.RUnlock()
	snap.IdleSeconds = int(snap.IdleFor(time.Now()).Seconds())
	return snap, true
}

// ScanIdle invokes fn with an immutable Snapshot of every window whose
// idle-time relative to now() is at least idleAtLeast. fn runs OUTSIDE any
// shard lock, so it may invoke Store APIs without deadlocking.
//
// Iteration order is not stable. Each shard is scanned under its read lock;
// snapshots are deep-copied before fn is called.
func (s *Store) ScanIdle(idleAtLeast time.Duration, fn func(Snapshot)) {
	if fn == nil {
		return
	}
	now := time.Now().UTC()
	// Per-shard collection so we don't hold any lock while invoking fn.
	for i := range s.shards {
		sh := &s.shards[i]
		var batch []Snapshot
		sh.mu.RLock()
		for _, w := range sh.windows {
			if now.Sub(w.LastSeen) < idleAtLeast {
				continue
			}
			batch = append(batch, w.snapshot())
		}
		sh.mu.RUnlock()
		for _, snap := range batch {
			// Mirror Store.Snapshot: populate IdleSeconds so SSE consumers
			// (e.g. the TriggerCard "Why" panel) see the correct idle figure
			// instead of the zero-value default.
			snap.IdleSeconds = int(snap.IdleFor(now).Seconds())
			fn(snap)
		}
	}
}

// Prune removes windows whose LastSeen is older than now() - olderThan.
// Returns the number of windows removed. Pruned anonymousIds are emitted on
// the pruned channel (best-effort, drops on full).
func (s *Store) Prune(olderThan time.Duration) int {
	cutoff := time.Now().UTC().Add(-olderThan)
	var removed int
	for i := range s.shards {
		sh := &s.shards[i]
		sh.mu.Lock()
		for id, w := range sh.windows {
			if w.LastSeen.Before(cutoff) {
				delete(sh.windows, id)
				removed++
				s.active.Add(-1)
				select {
				case s.prunedCh <- id:
				default:
				}
			}
		}
		sh.mu.Unlock()
	}
	return removed
}

// Reset drops all window data from every shard and resets the active counter
// to zero. Intended for demo-reset paths where stale aggregations must not
// pollute the next demo run. Returns the number of windows discarded.
// Concurrent callers (e.g. the idle ticker) will see empty shards and create
// new windows on next access.
func (s *Store) Reset() int {
	var total int
	for i := range s.shards {
		sh := &s.shards[i]
		sh.mu.Lock()
		n := len(sh.windows)
		sh.windows = make(map[string]*UserWindow)
		total += n
		sh.mu.Unlock()
	}
	s.active.Store(0)
	return total
}

// Active returns the current count of resident windows. Atomic snapshot;
// concurrent inserts/evictions may make it slightly stale.
func (s *Store) Active() int {
	return int(s.active.Load())
}

// PrunedChan returns the channel that receives pruned anonymousIds. Consumers
// translate these into SSE notifications (§3.3).
func (s *Store) PrunedChan() <-chan string {
	return s.prunedCh
}

// RunPruner runs an idle pruner loop until ctx is cancelled. Idle TTL is
// idleTTL; the check fires every `every`. Safe to call once per Store.
//
// The pruner uses a ticker rather than time.AfterFunc to avoid goroutine
// proliferation. ctx cancellation is checked between ticks; the call returns
// only when ctx is done.
func (s *Store) RunPruner(ctx context.Context, idleTTL time.Duration, every time.Duration) {
	if every <= 0 {
		every = time.Minute
	}
	if idleTTL <= 0 {
		idleTTL = 15 * time.Minute
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Prune(idleTTL)
		}
	}
}

// SnapshotAll returns deep-copied snapshots of every active window across
// all shards. Useful for dashboard rehydration after a browser refresh.
// IdleSeconds is populated for each snapshot, mirroring Snapshot().
func (s *Store) SnapshotAll() []Snapshot {
	out := make([]Snapshot, 0, s.Active())
	now := time.Now().UTC()
	for i := range s.shards {
		sh := &s.shards[i]
		sh.mu.RLock()
		for _, w := range sh.windows {
			snap := w.snapshot()
			snap.IdleSeconds = int(snap.IdleFor(now).Seconds())
			out = append(out, snap)
		}
		sh.mu.RUnlock()
	}
	return out
}

// evictGlobalOldest finds the globally least-recently-touched window across
// all shards and removes it. Returns true if an eviction occurred.
//
// Implementation: scan all shards under read lock to find the victim; then
// re-acquire the victim's shard under write lock and delete (re-checking
// lastTouched in case the entry was touched between scan and delete). This
// avoids holding multiple shard locks simultaneously.
func (s *Store) evictGlobalOldest() bool {
	var (
		victimShard *shard
		victimID    string
		oldest      time.Time
		found       bool
	)
	for i := range s.shards {
		sh := &s.shards[i]
		sh.mu.RLock()
		for id, w := range sh.windows {
			if !found || w.lastTouched.Before(oldest) {
				oldest = w.lastTouched
				victimID = id
				victimShard = sh
				found = true
			}
		}
		sh.mu.RUnlock()
	}
	if !found {
		return false
	}
	victimShard.mu.Lock()
	defer victimShard.mu.Unlock()
	w, ok := victimShard.windows[victimID]
	if !ok {
		// Already gone (pruned concurrently). Caller will retry the cap
		// check on the next loop iteration.
		return true
	}
	// Avoid evicting an entry that was touched after our scan. If it was,
	// caller's loop will pick another victim next round.
	if w.lastTouched.After(oldest) {
		return true
	}
	delete(victimShard.windows, victimID)
	s.active.Add(-1)
	select {
	case s.prunedCh <- victimID:
	default:
	}
	return true
}
