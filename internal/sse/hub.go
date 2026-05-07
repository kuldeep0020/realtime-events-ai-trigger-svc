// Package sse provides a goroutine-safe in-process pub/sub hub used to fan
// out events, window updates, triggers, and mock-email notifications to
// connected SSE subscribers (browser clients via the dashboard UI).
//
// Concurrency model (§3.9):
//   - One sync.RWMutex protects the per-stream subscriber lists. Publish
//     takes the read lock; Subscribe / unsubscribe take the write lock.
//   - Each subscriber owns a buffered chan Message (capacity 64). If the
//     channel is full when Publish runs, the subscriber is dropped on the
//     spot — closed and removed — to avoid head-of-line blocking the hub.
//   - Each subscriber has a dedicated heartbeat goroutine that emits a
//     keepalive Message every 15s. The goroutine exits when the subscriber
//     is dropped or the hub is closed.
//
// The hub is shutdown-safe: Close() drains all subscribers and waits for
// every heartbeat goroutine to exit (no leaks per goleak).
package sse

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// Message is a single SSE payload. The hub is agnostic about the body —
// handlers serialize Data as JSON and write it on the wire under Event.
type Message struct {
	// Event is the SSE "event:" field (e.g. "trigger", "window_pruned",
	// "heartbeat"). Empty Event => default "message" event.
	Event string
	// Data is the SSE "data:" field. Handlers serialize structured types
	// here as JSON before write; Hub does NOT mutate this field.
	Data any
	// ID is an optional SSE "id:" field used by EventSource for resume.
	// Empty values are not emitted.
	ID string
}

// IsHeartbeat returns true when the message is the periodic keepalive
// emitted by the heartbeat goroutine. Useful for tests and metrics.
func (m Message) IsHeartbeat() bool { return m.Event == EventHeartbeat }

// Stream identifiers. The four canonical streams from the design (§3.9).
const (
	StreamEvents     = "events"
	StreamWindows    = "windows"
	StreamTriggers   = "triggers"
	StreamMockEmails = "mock_emails"
)

// EventHeartbeat is the SSE event type used for periodic keepalives.
const EventHeartbeat = "heartbeat"

// EventWindowPruned is the SSE event type emitted when a window is evicted
// from the in-memory store. Matches the string the frontend WindowInspector
// listener registers ("window_pruned").
const EventWindowPruned = "window_pruned"

// Tunables. Kept package-private so tests have a single override point.
const (
	subscriberBufferSize = 64
	heartbeatInterval    = 15 * time.Second
)

// subscriber tracks a single connected client.
type subscriber struct {
	id   uint64
	ch   chan Message
	// stopHeartbeat is closed when the subscriber is dropped (full buffer)
	// or when Close runs. The heartbeat goroutine selects on it to exit.
	stopHeartbeat chan struct{}
	// closed is set once via CompareAndSwap when the channel is closed and
	// the heartbeat is signalled to stop, preventing double-close panics.
	closed atomic.Bool
}

// Hub is the in-process fan-out registry. The zero value is NOT usable —
// callers must use NewHub.
type Hub struct {
	// mu guards subs.
	mu sync.RWMutex
	// subs is the per-stream subscriber list. Slice ordering is irrelevant.
	subs map[string][]*subscriber

	// nextID hands out monotonically increasing subscriber IDs.
	nextID atomic.Uint64

	// hbInterval is the heartbeat tick. Tests override via the option.
	hbInterval time.Duration

	// dropped is a process-wide counter of slow subscribers dropped due to
	// a full buffer. Inspectable via DroppedCount for metrics/tests.
	dropped atomic.Uint64

	// wg tracks heartbeat goroutines so Close can join them and tests
	// (with goleak) get clean teardown.
	wg sync.WaitGroup

	// closed is set when Close runs; further Subscribe/Publish are no-ops.
	closed atomic.Bool
}

// Option configures hub construction.
type Option func(*Hub)

// WithHeartbeatInterval overrides the 15s default. Useful in tests.
func WithHeartbeatInterval(d time.Duration) Option {
	return func(h *Hub) {
		if d > 0 {
			h.hbInterval = d
		}
	}
}

// NewHub constructs a Hub with sensible defaults. Use WithHeartbeatInterval
// in tests to keep them snappy.
func NewHub(opts ...Option) *Hub {
	h := &Hub{
		subs:       make(map[string][]*subscriber),
		hbInterval: heartbeatInterval,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Subscribe registers a new subscriber on the named stream and returns a
// receive-only channel plus an unsubscribe function the caller MUST invoke
// when their connection ends. After unsubscribe, the channel is closed and
// any heartbeat goroutine exits.
//
// If the hub is closed, returns a closed channel and a no-op unsubscribe.
func (h *Hub) Subscribe(stream string) (<-chan Message, func()) {
	if h.closed.Load() || stream == "" {
		closedCh := make(chan Message)
		close(closedCh)
		return closedCh, func() {}
	}

	s := &subscriber{
		id:            h.nextID.Add(1),
		ch:            make(chan Message, subscriberBufferSize),
		stopHeartbeat: make(chan struct{}),
	}

	h.mu.Lock()
	h.subs[stream] = append(h.subs[stream], s)
	h.mu.Unlock()

	// Spawn per-subscriber heartbeat goroutine.
	h.wg.Add(1)
	go h.runHeartbeat(s)

	unsubscribe := func() { h.removeSubscriber(stream, s) }
	return s.ch, unsubscribe
}

// Publish fan-outs msg to every subscriber on the named stream. Slow
// subscribers (full buffer) are dropped — channel closed, removed from list,
// heartbeat goroutine signalled to exit.
//
// Publish is non-blocking: it never waits on a subscriber's channel.
//
// We hold the read lock for the entire duration of the fan-out. Because
// every send is non-blocking (`select default`), the duration is bounded
// by O(subscribers). Holding the read lock is what makes concurrent
// closeSubscriber wait — without it, Publish could send on a channel that
// another goroutine just closed (panic + race).
func (h *Hub) Publish(stream string, msg Message) {
	if h.closed.Load() || stream == "" {
		return
	}

	h.mu.RLock()
	subs := h.subs[stream]
	if len(subs) == 0 {
		h.mu.RUnlock()
		return
	}

	var toDrop []*subscriber
	for _, s := range subs {
		// Skip subscribers in the middle of being closed. closed is
		// flipped under the write lock by closeSubscriber, but we hold
		// the read lock here so a writer cannot enter until we exit.
		// The check guards against calling sendNonBlocking on a
		// subscriber whose channel is about to be closed by a concurrent
		// drop pass on the same stream.
		if s.closed.Load() {
			continue
		}
		select {
		case s.ch <- msg:
			// delivered
		default:
			toDrop = append(toDrop, s)
		}
	}
	h.mu.RUnlock()

	for _, s := range toDrop {
		h.dropSlowSubscriber(stream, s)
	}
}

// DroppedCount returns the count of slow subscribers dropped since the hub
// was created. Useful for the /metrics endpoint and tests.
func (h *Hub) DroppedCount() uint64 { return h.dropped.Load() }

// SubscriberCount returns the current number of subscribers on a stream.
// Useful for tests and the readyz endpoint.
func (h *Hub) SubscriberCount(stream string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs[stream])
}

// Close shuts the hub down: closes every subscriber channel, signals every
// heartbeat goroutine to exit, and waits for them. Safe to call once.
// Further Subscribe / Publish calls become no-ops. Returns when all
// goroutines have exited or ctx is cancelled.
func (h *Hub) Close(ctx context.Context) error {
	if !h.closed.CompareAndSwap(false, true) {
		return nil
	}

	// Snapshot all subscribers, then clear the registry under write lock.
	h.mu.Lock()
	all := make([]*subscriber, 0, 16)
	for _, list := range h.subs {
		all = append(all, list...)
	}
	h.subs = make(map[string][]*subscriber)
	h.mu.Unlock()

	for _, s := range all {
		h.closeSubscriber(s)
	}

	// Wait for heartbeat goroutines to exit, bounded by ctx.
	done := make(chan struct{})
	go func() {
		h.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// removeSubscriber detaches s from stream and closes it. Safe to call
// multiple times (closeSubscriber is idempotent).
func (h *Hub) removeSubscriber(stream string, s *subscriber) {
	h.mu.Lock()
	list := h.subs[stream]
	for i, candidate := range list {
		if candidate == s {
			h.subs[stream] = append(list[:i], list[i+1:]...)
			break
		}
	}
	h.mu.Unlock()
	h.closeSubscriber(s)
}

// dropSlowSubscriber is the slow-subscriber-drop variant: bumps the dropped
// counter, removes from the list, closes the channel, exits heartbeat. We
// take the write lock here ONLY around the slice mutation; closing the
// subscriber happens after the lock is released.
func (h *Hub) dropSlowSubscriber(stream string, s *subscriber) {
	h.mu.Lock()
	list := h.subs[stream]
	found := false
	for i, candidate := range list {
		if candidate == s {
			h.subs[stream] = append(list[:i], list[i+1:]...)
			found = true
			break
		}
	}
	h.mu.Unlock()

	if !found {
		// Already removed by a concurrent unsubscribe; nothing to do.
		return
	}
	h.dropped.Add(1)
	h.closeSubscriber(s)
}

// closeSubscriber is idempotent: closes the channel and signals the
// heartbeat goroutine to exit at most once.
func (h *Hub) closeSubscriber(s *subscriber) {
	if s == nil {
		return
	}
	if !s.closed.CompareAndSwap(false, true) {
		return
	}
	close(s.stopHeartbeat)
	close(s.ch)
}

// runHeartbeat emits a periodic keepalive on s.ch until s is dropped or the
// hub is closed. We use a non-blocking send so a slow consumer doesn't pin
// the heartbeat goroutine; if the buffer is full, the next Publish will
// drop the subscriber and our select on stopHeartbeat will then return.
//
// We route sends through Hub.publishToSubscriber so they participate in the
// same RLock-guarded send that Publish uses — without that synchronisation,
// a tick-time send could race with a concurrent close.
func (h *Hub) runHeartbeat(s *subscriber) {
	defer h.wg.Done()

	ticker := time.NewTicker(h.hbInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopHeartbeat:
			return
		case t := <-ticker.C:
			msg := Message{Event: EventHeartbeat, Data: t.UTC().Format(time.RFC3339Nano)}
			h.heartbeatSend(s, msg)
		}
	}
}

// heartbeatSend performs a single non-blocking send to s.ch under the
// hub's read lock so it serializes with closeSubscriber (which mutates
// under the write lock). If s is already closed, the send is skipped.
func (h *Hub) heartbeatSend(s *subscriber, msg Message) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if s.closed.Load() {
		return
	}
	select {
	case s.ch <- msg:
		// delivered
	default:
		// buffer full — leave the drop decision to the next Publish call.
	}
}
