package sse

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"
)

// TestMain runs goleak to assert no goroutines leak across the test binary.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// --- Subscribe + Publish: parallel subscribers, all receive ---

func TestHub_PublishToParallelSubscribers(t *testing.T) {
	t.Parallel()
	hub := NewHub(WithHeartbeatInterval(time.Hour))
	defer func() {
		_ = hub.Close(context.Background())
	}()

	const n = 8
	chs := make([]<-chan Message, n)
	cancels := make([]func(), n)
	for i := 0; i < n; i++ {
		chs[i], cancels[i] = hub.Subscribe(StreamTriggers)
	}

	// Drain pre-published messages? — none yet. Publish one.
	hub.Publish(StreamTriggers, Message{Event: "trigger", Data: map[string]any{"i": 1}})

	for i, ch := range chs {
		select {
		case m := <-ch:
			if m.Event != "trigger" {
				t.Errorf("subscriber %d: expected event=trigger, got %q", i, m.Event)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d: timeout waiting for message", i)
		}
	}

	for _, c := range cancels {
		c()
	}
}

// --- Slow subscriber dropped on full buffer ---

func TestHub_SlowSubscriberDropped(t *testing.T) {
	t.Parallel()
	hub := NewHub(WithHeartbeatInterval(time.Hour))
	defer func() {
		_ = hub.Close(context.Background())
	}()

	// Subscriber A: never reads.
	chSlow, _ := hub.Subscribe(StreamEvents)
	// Subscriber B: reads everything.
	chFast, cancelB := hub.Subscribe(StreamEvents)
	defer cancelB()

	// Fill the slow subscriber's buffer.
	for i := 0; i < subscriberBufferSize; i++ {
		hub.Publish(StreamEvents, Message{Event: "e", Data: i})
	}
	// Drain the fast one in parallel so it never blocks.
	go func() {
		for range chFast {
		}
	}()

	// Wait briefly to let buffer fill.
	time.Sleep(20 * time.Millisecond)

	// One more publish — slow one is full and gets dropped.
	hub.Publish(StreamEvents, Message{Event: "drop-trigger", Data: "x"})

	// Allow drop bookkeeping to settle.
	time.Sleep(20 * time.Millisecond)

	if got := hub.DroppedCount(); got != 1 {
		t.Errorf("expected 1 dropped subscriber, got %d", got)
	}
	if got := hub.SubscriberCount(StreamEvents); got != 1 {
		t.Errorf("expected 1 remaining subscriber, got %d", got)
	}
	// The slow channel should now be closed.
	timeout := time.After(time.Second)
	for {
		select {
		case _, ok := <-chSlow:
			if !ok {
				return // drained + closed → success
			}
		case <-timeout:
			t.Fatal("expected slow subscriber channel to be closed within 1s")
		}
	}
}

// --- Heartbeat ticks fire ---

func TestHub_HeartbeatEmitted(t *testing.T) {
	t.Parallel()
	hub := NewHub(WithHeartbeatInterval(20 * time.Millisecond))
	defer func() {
		_ = hub.Close(context.Background())
	}()

	ch, cancel := hub.Subscribe(StreamWindows)
	defer cancel()

	deadline := time.After(2 * time.Second)
	for {
		select {
		case msg := <-ch:
			if msg.IsHeartbeat() {
				return
			}
		case <-deadline:
			t.Fatal("did not receive heartbeat within 2s")
		}
	}
}

// --- Unsubscribe stops heartbeat goroutine ---

func TestHub_UnsubscribeStopsHeartbeat(t *testing.T) {
	t.Parallel()
	hub := NewHub(WithHeartbeatInterval(10 * time.Millisecond))
	defer func() {
		_ = hub.Close(context.Background())
	}()

	ch, cancel := hub.Subscribe(StreamMockEmails)
	cancel()

	// Reading from the closed channel should return zero/false.
	select {
	case _, ok := <-ch:
		if ok {
			t.Errorf("expected channel to be drained+closed, got message")
		}
	case <-time.After(time.Second):
		t.Fatal("channel not closed within 1s after unsubscribe")
	}
}

// --- Close drains and waits for heartbeat goroutines ---

func TestHub_CloseShutdownNoLeak(t *testing.T) {
	t.Parallel()
	hub := NewHub(WithHeartbeatInterval(10 * time.Millisecond))

	const n = 5
	chs := make([]<-chan Message, n)
	for i := 0; i < n; i++ {
		chs[i], _ = hub.Subscribe(StreamTriggers)
	}

	if err := hub.Close(context.Background()); err != nil {
		t.Fatalf("Close returned %v", err)
	}

	// Every channel must be closed.
	for i, ch := range chs {
		select {
		case _, ok := <-ch:
			if ok {
				t.Errorf("subscriber %d channel still open after Close", i)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d channel not closed within 1s", i)
		}
	}

	// Subsequent Publish/Subscribe are no-ops.
	hub.Publish(StreamTriggers, Message{Event: "ignored"})

	post, cancel := hub.Subscribe(StreamTriggers)
	defer cancel()
	select {
	case _, ok := <-post:
		if ok {
			t.Errorf("expected closed channel after hub Close")
		}
	case <-time.After(100 * time.Millisecond):
		t.Errorf("subscribing post-Close should yield a pre-closed channel")
	}
}

// --- Close with deadline expiry ---

func TestHub_CloseRespectsContextDeadline(t *testing.T) {
	t.Parallel()
	hub := NewHub(WithHeartbeatInterval(time.Hour))
	_, _ = hub.Subscribe(StreamEvents)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	// With long heartbeat interval, Close should still complete promptly
	// because heartbeat goroutines select on stopHeartbeat too.
	err := hub.Close(ctx)
	if err != nil && err != context.DeadlineExceeded {
		t.Errorf("expected nil or DeadlineExceeded, got %v", err)
	}
}

// --- Concurrent publish/subscribe stress ---

func TestHub_ConcurrentPublishSubscribe(t *testing.T) {
	t.Parallel()
	hub := NewHub(WithHeartbeatInterval(time.Hour))
	defer func() {
		_ = hub.Close(context.Background())
	}()

	var (
		received atomic.Uint64
		wg       sync.WaitGroup
	)

	const subscribers = 4
	const messages = 100

	for i := 0; i < subscribers; i++ {
		ch, cancel := hub.Subscribe(StreamEvents)
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer cancel()
			seen := 0
			for m := range ch {
				if m.Event == "stop" {
					return
				}
				if !m.IsHeartbeat() {
					received.Add(1)
					seen++
					if seen >= messages {
						return
					}
				}
			}
		}()
	}

	// Allow subscribers to register.
	time.Sleep(20 * time.Millisecond)

	for i := 0; i < messages; i++ {
		hub.Publish(StreamEvents, Message{Event: "e", Data: i})
	}
	// Tell subscribers to exit.
	for i := 0; i < subscribers; i++ {
		hub.Publish(StreamEvents, Message{Event: "stop"})
	}
	wg.Wait()

	if got := received.Load(); got == 0 {
		t.Fatalf("expected non-zero deliveries, got %d", got)
	}
}

// --- Empty stream name is rejected ---

func TestHub_EmptyStreamRejected(t *testing.T) {
	t.Parallel()
	hub := NewHub()
	defer func() {
		_ = hub.Close(context.Background())
	}()

	ch, cancel := hub.Subscribe("")
	defer cancel()
	select {
	case _, ok := <-ch:
		if ok {
			t.Errorf("expected closed channel for empty stream")
		}
	case <-time.After(100 * time.Millisecond):
		t.Errorf("expected pre-closed channel for empty stream")
	}
}
