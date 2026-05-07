package consumer

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/apache/pulsar-client-go/pulsar"
	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/event"
)

// ---- mock types ----

// mockMessage implements pulsar.Message for testing.
type mockMessage struct {
	payload    []byte
	properties map[string]string
	id         mockMessageID
}

type mockMessageID struct{ key string }

func (m mockMessageID) Serialize() []byte        { return []byte(m.key) }
func (m mockMessageID) LedgerID() int64          { return 0 }
func (m mockMessageID) EntryID() int64           { return 0 }
func (m mockMessageID) BatchIdx() int32          { return 0 }
func (m mockMessageID) PartitionIdx() int32      { return 0 }
func (m mockMessageID) BatchSize() int32         { return 0 }
func (m mockMessageID) String() string           { return m.key }

func (m *mockMessage) Topic() string                                          { return "test-topic" }
func (m *mockMessage) ProducerName() string                                   { return "producer" }
func (m *mockMessage) Properties() map[string]string                         { return m.properties }
func (m *mockMessage) Payload() []byte                                       { return m.payload }
func (m *mockMessage) ID() pulsar.MessageID                                  { return m.id }
func (m *mockMessage) PublishTime() time.Time                                 { return time.Now() }
func (m *mockMessage) EventTime() time.Time                                   { return time.Now() }
func (m *mockMessage) Key() string                                           { return "" }
func (m *mockMessage) OrderingKey() string                                   { return "" }
func (m *mockMessage) GetSchemaValue(v interface{}) error                     { return nil }
func (m *mockMessage) Index() *uint64                                        { return nil }
func (m *mockMessage) BrokerPublishTime() *time.Time                          { return nil }
func (m *mockMessage) IsReplicated() bool                                    { return false }
func (m *mockMessage) GetReplicatedFrom() string                             { return "" }
func (m *mockMessage) GetEncryptionContext() *pulsar.EncryptionContext        { return nil }
func (m *mockMessage) SchemaVersion() []byte                                 { return nil }
func (m *mockMessage) RedeliveryCount() uint32                               { return 0 }

// mockConsumer implements pulsarConsumer, returning messages from a queue then
// blocking on ctx cancellation.
type mockConsumer struct {
	msgs    []pulsar.Message
	idx     int
	acked   []pulsar.Message
	nacked  []pulsar.Message
	closed  bool
}

func (c *mockConsumer) Receive(ctx context.Context) (pulsar.Message, error) {
	if c.idx < len(c.msgs) {
		msg := c.msgs[c.idx]
		c.idx++
		return msg, nil
	}
	// Block until context is cancelled.
	<-ctx.Done()
	return nil, ctx.Err()
}

func (c *mockConsumer) Ack(msg pulsar.Message) error {
	c.acked = append(c.acked, msg)
	return nil
}
func (c *mockConsumer) Nack(msg pulsar.Message) { c.nacked = append(c.nacked, msg) }
func (c *mockConsumer) Close()                  { c.closed = true }

// ---- helpers ----

func validEventPayload(t *testing.T) []byte {
	t.Helper()
	ev := event.Event{
		Type:        "track",
		Channel:     "browser",
		AnonymousID: "anon-001",
		MessageID:   "msg-001",
		Event:       "Page Viewed",
	}
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	return b
}

func runUntilDrained(t *testing.T, mc *mockConsumer, msgCount int) ([]ProcessedEvent, *Consumer) {
	t.Helper()
	out := make(chan ProcessedEvent, msgCount+2)
	log := slog.Default()
	c := newFromSub(mc, out, log)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		c.Run(ctx)
		close(done)
	}()

	var events []ProcessedEvent
	timeout := time.After(1 * time.Second)
outer:
	for {
		select {
		case pe := <-out:
			events = append(events, pe)
			if len(events) >= msgCount {
				cancel()
				break outer
			}
		case <-timeout:
			cancel()
			break outer
		}
	}
	<-done
	return events, c
}

// ---- tests ----

// TestAckOnSuccess verifies a valid event is forwarded on the channel and acked.
func TestAckOnSuccess(t *testing.T) {
	payload := validEventPayload(t)
	msg := &mockMessage{
		payload:    payload,
		properties: map[string]string{"writeKey": "wk-1", "sourceId": "src-1"},
		id:         mockMessageID{key: "msg-id-1"},
	}
	mc := &mockConsumer{msgs: []pulsar.Message{msg}}

	events, c := runUntilDrained(t, mc, 1)
	_ = c

	if len(events) != 1 {
		t.Fatalf("expected 1 processed event, got %d", len(events))
	}
	if events[0].WriteKey != "wk-1" {
		t.Errorf("WriteKey: want wk-1, got %q", events[0].WriteKey)
	}
	if events[0].SourceID != "src-1" {
		t.Errorf("SourceID: want src-1, got %q", events[0].SourceID)
	}
	if events[0].Event == nil {
		t.Fatal("Event is nil")
	}
	if events[0].Event.AnonymousID != "anon-001" {
		t.Errorf("AnonymousID: want anon-001, got %q", events[0].Event.AnonymousID)
	}
	if len(mc.acked) != 1 {
		t.Errorf("expected 1 ack, got %d", len(mc.acked))
	}
}

// TestAckOnParseError verifies that a message with invalid JSON is acked (not
// nacked) so it never re-enters the loop.
func TestAckOnParseError(t *testing.T) {
	msg := &mockMessage{
		payload:    []byte(`not valid json{{`),
		properties: map[string]string{"writeKey": "wk-bad"},
		id:         mockMessageID{key: "msg-bad"},
	}
	mc := &mockConsumer{msgs: []pulsar.Message{msg}}

	out := make(chan ProcessedEvent, 4)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	c := newFromSub(mc, out, slog.Default())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	<-done

	if len(out) != 0 {
		t.Errorf("expected 0 events forwarded, got %d", len(out))
	}
	if len(mc.acked) != 1 {
		t.Errorf("expected 1 ack on parse error, got %d", len(mc.acked))
	}
	if len(mc.nacked) != 0 {
		t.Errorf("expected 0 nacks on parse error, got %d", len(mc.nacked))
	}
	_, parseErrs, _ := c.Stats()
	if parseErrs != 1 {
		t.Errorf("expected parseErrs=1, got %d", parseErrs)
	}
}

// TestPanicRecovery verifies that a message whose Payload() panics is recovered,
// acked, and does not prevent subsequent messages from being processed.
func TestPanicRecovery(t *testing.T) {
	validPayload := validEventPayload(t)

	// panicConsumer delivers: (1) a message that panics in Payload(), then
	// (2) a normal valid message.
	panicCons := &panicConsumer{normalMsg: &mockMessage{
		payload:    validPayload,
		properties: map[string]string{"writeKey": "wk-ok"},
		id:         mockMessageID{key: "normal-after-panic"},
	}}

	out := make(chan ProcessedEvent, 4)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	c := newFromSub(panicCons, out, slog.Default())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	<-done

	// The normal message should have been processed.
	if len(out) < 1 {
		t.Error("expected at least 1 processed event after panic recovery")
	}
	// Panic message should have been acked.
	if panicCons.panicMsgAcked < 1 {
		t.Error("expected panic message to be acked")
	}
}

// panicConsumer delivers a panic-inducing message then a normal one.
type panicConsumer struct {
	normalMsg     pulsar.Message
	idx           int
	panicMsgAcked int
	acked         []pulsar.Message
}

func (c *panicConsumer) Receive(ctx context.Context) (pulsar.Message, error) {
	switch c.idx {
	case 0:
		c.idx++
		return &panicPayloadMsg{mockMessage: mockMessage{id: mockMessageID{key: "panic-msg"}}}, nil
	case 1:
		c.idx++
		return c.normalMsg, nil
	default:
		<-ctx.Done()
		return nil, ctx.Err()
	}
}

func (c *panicConsumer) Ack(msg pulsar.Message) error {
	if _, ok := msg.(*panicPayloadMsg); ok {
		c.panicMsgAcked++
	}
	c.acked = append(c.acked, msg)
	return nil
}
func (c *panicConsumer) Nack(msg pulsar.Message) {}
func (c *panicConsumer) Close()                  {}

// panicPayloadMsg panics when Payload() is called.
type panicPayloadMsg struct{ mockMessage }

func (p *panicPayloadMsg) Payload() []byte { panic("deliberate test panic in Payload()") }

// TestGracefulCtxShutdown verifies that cancelling the context stops the loop.
func TestGracefulCtxShutdown(t *testing.T) {
	mc := &mockConsumer{msgs: nil} // no messages — will block on Receive

	out := make(chan ProcessedEvent, 4)
	ctx, cancel := context.WithCancel(context.Background())

	c := newFromSub(mc, out, slog.Default())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()

	// Cancel almost immediately.
	cancel()

	select {
	case <-done:
		// Good — Run exited.
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after context cancellation")
	}
}

// TestMissingProperties verifies that absent writeKey/sourceId do not crash
// and result in empty strings in the output.
func TestMissingProperties(t *testing.T) {
	payload := validEventPayload(t)
	msg := &mockMessage{
		payload:    payload,
		properties: nil, // no properties at all
		id:         mockMessageID{key: "msg-noprops"},
	}
	mc := &mockConsumer{msgs: []pulsar.Message{msg}}

	events, _ := runUntilDrained(t, mc, 1)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].WriteKey != "" {
		t.Errorf("expected empty WriteKey, got %q", events[0].WriteKey)
	}
	if events[0].SourceID != "" {
		t.Errorf("expected empty SourceID, got %q", events[0].SourceID)
	}
}
