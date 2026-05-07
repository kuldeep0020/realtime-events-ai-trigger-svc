package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/apache/pulsar-client-go/pulsar"
	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/event"
)

const defaultSubscriptionName = "realtime-ai-trigger-svc-v1"

// Config holds all configuration needed to connect to a Pulsar cluster and
// subscribe to the source-events topic.
type Config struct {
	// URL is the Pulsar broker URL, e.g. "pulsar+ssl://host:6651".
	URL string

	// Token is the JWT used for authentication. When empty, no auth is applied
	// (useful for local dev with no auth).
	Token string

	// Topic is the fully-qualified Pulsar topic name.
	Topic string

	// SubscriptionName is the Shared subscription name. Defaults to
	// "realtime-ai-trigger-svc-v1" when empty.
	SubscriptionName string

	// ConnectTimeout is passed to the Pulsar client as the connection timeout.
	// Zero uses the library default (30s).
	ConnectTimeout time.Duration

	// OperationTimeout is the per-operation timeout for Pulsar client calls.
	// Zero uses the library default (30s).
	OperationTimeout time.Duration

	// TLSTrustCertsFile is an optional path to a PEM-encoded CA cert used to
	// trust the broker's TLS certificate. Required for self-signed brokers
	// (e.g. the local Docker Pulsar in dev/demo). Empty falls back to the
	// system trust store, which is correct for the StreamNative production
	// cluster.
	TLSTrustCertsFile string

	// TLSValidateHostname controls hostname verification on the broker cert.
	// Defaults to true. Set to false only when intentionally connecting to a
	// broker whose cert SANs do not include the address you connect with
	// (rare; usually a misconfiguration).
	TLSValidateHostname bool

	// TLSAllowInsecure disables broker cert verification entirely. Should
	// remain false in all real deployments; exposed only for emergency
	// debugging. Defaults to false.
	TLSAllowInsecure bool
}

// Consumer wraps a Pulsar client and consumer and exposes a Run loop that
// deserializes events and pushes them to an output channel.
type Consumer struct {
	cfg    Config
	client pulsar.Client
	sub    pulsarConsumer // interface for testability
	out    chan<- ProcessedEvent
	log    *slog.Logger

	// counters — read with atomic, written in Run goroutine
	received  atomic.Int64
	parseErrs atomic.Int64
	sent      atomic.Int64
}

// pulsarConsumer is the subset of pulsar.Consumer used by the Run loop.
// It is satisfied by both the real pulsar.Consumer and test mocks.
// Note: Ack returns an error in the real Pulsar client; we log but do not
// propagate it since the only recourse is re-delivery which we want to avoid.
type pulsarConsumer interface {
	Receive(ctx context.Context) (pulsar.Message, error)
	Ack(msg pulsar.Message) error
	Nack(msg pulsar.Message)
	Close()
}

// New creates a Pulsar client, subscribes to the configured topic, and returns
// a Consumer ready to run. The caller must call Close() when done.
func New(ctx context.Context, cfg Config, out chan<- ProcessedEvent, log *slog.Logger) (*Consumer, error) {
	if cfg.SubscriptionName == "" {
		cfg.SubscriptionName = defaultSubscriptionName
	}
	if log == nil {
		log = slog.Default()
	}

	connectTimeout := cfg.ConnectTimeout
	if connectTimeout == 0 {
		connectTimeout = 30 * time.Second
	}
	operationTimeout := cfg.OperationTimeout
	if operationTimeout == 0 {
		operationTimeout = 30 * time.Second
	}

	clientOpts := pulsar.ClientOptions{
		URL:                        cfg.URL,
		ConnectionTimeout:          connectTimeout,
		OperationTimeout:           operationTimeout,
		TLSTrustCertsFilePath:      cfg.TLSTrustCertsFile,
		TLSValidateHostname:        cfg.TLSValidateHostname,
		TLSAllowInsecureConnection: cfg.TLSAllowInsecure,
	}

	if cfg.Token != "" {
		clientOpts.Authentication = pulsar.NewAuthenticationToken(cfg.Token)
	}

	client, err := pulsar.NewClient(clientOpts)
	if err != nil {
		return nil, fmt.Errorf("creating pulsar client: %w", err)
	}

	sub, err := client.Subscribe(pulsar.ConsumerOptions{
		Topic:            cfg.Topic,
		SubscriptionName: cfg.SubscriptionName,
		Type:             pulsar.Shared,
	})
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("subscribing to topic %s: %w", cfg.Topic, err)
	}

	return &Consumer{
		cfg:    cfg,
		client: client,
		sub:    sub,
		out:    out,
		log:    log,
	}, nil
}

// newFromSub creates a Consumer from an already-subscribed pulsarConsumer. Used
// by tests to inject a mock without needing a real broker.
func newFromSub(sub pulsarConsumer, out chan<- ProcessedEvent, log *slog.Logger) *Consumer {
	if log == nil {
		log = slog.Default()
	}
	return &Consumer{sub: sub, out: out, log: log}
}

// Run starts the message receive loop. It blocks until ctx is cancelled.
// Each message is deserialized and forwarded on the output channel; on parse
// error the message is acked-and-skipped so it never re-enters the loop.
// Panics inside the loop are recovered and logged.
func (c *Consumer) Run(ctx context.Context) {
	for {
		// Check context before blocking on Receive.
		if ctx.Err() != nil {
			return
		}

		msg, err := c.sub.Receive(ctx)
		if err != nil {
			// Context cancellation surfaces here as an error.
			if ctx.Err() != nil {
				return
			}
			c.log.Error("pulsar receive error", "err", err)
			continue
		}

		c.received.Add(1)
		c.processMessage(ctx, msg)
	}
}

// processMessage handles a single Pulsar message. It is split out of Run so
// the deferred panic recovery works correctly.
func (c *Consumer) processMessage(ctx context.Context, msg pulsar.Message) {
	defer func() {
		if r := recover(); r != nil {
			c.log.Error("panic in processMessage — acking and skipping",
				"panic", fmt.Sprintf("%v", r),
				"messageID", messageIDStr(msg),
			)
			c.parseErrs.Add(1)
			if ackErr := c.sub.Ack(msg); ackErr != nil {
				c.log.Warn("ack failed after panic", "err", ackErr)
			}
		}
	}()

	props := msg.Properties()

	writeKey := safeGet(props, "writeKey")
	sourceID := safeGet(props, "sourceId")
	msgID := safeGet(props, "messageId")
	if msgID == "" {
		msgID = messageIDStr(msg)
	}

	var ev event.Event
	if err := json.Unmarshal(msg.Payload(), &ev); err != nil {
		c.log.Warn("parse error — acking and skipping",
			"err", err,
			"messageID", msgID,
			"writeKey", writeKey,
		)
		c.parseErrs.Add(1)
		if ackErr := c.sub.Ack(msg); ackErr != nil {
			c.log.Warn("ack failed after parse error", "err", ackErr)
		}
		return
	}

	pe := ProcessedEvent{
		PulsarMessageID: msgID,
		WriteKey:        writeKey,
		SourceID:        sourceID,
		Event:           &ev,
		ReceivedAt:      time.Now().UTC(),
	}

	// Attempt to enqueue; if the context is cancelled while waiting, ack anyway
	// to avoid re-delivery of a message we partially processed.
	select {
	case c.out <- pe:
		if ackErr := c.sub.Ack(msg); ackErr != nil {
			c.log.Warn("ack failed after enqueue", "err", ackErr)
		}
		c.sent.Add(1)
	case <-ctx.Done():
		// Ack so we don't re-deliver on next startup; the downstream should
		// replay from Postgres if needed.
		if ackErr := c.sub.Ack(msg); ackErr != nil {
			c.log.Warn("ack failed on ctx cancel", "err", ackErr)
		}
	}
}

// Close stops the Pulsar subscription and client cleanly.
func (c *Consumer) Close() {
	if c.sub != nil {
		c.sub.Close()
	}
	if c.client != nil {
		c.client.Close()
	}
}

// Stats returns a snapshot of counters for observability.
func (c *Consumer) Stats() (received, parseErrs, sent int64) {
	return c.received.Load(), c.parseErrs.Load(), c.sent.Load()
}

// safeGet returns the value for key from props, or "" if absent/nil.
func safeGet(props map[string]string, key string) string {
	if props == nil {
		return ""
	}
	return props[key]
}

// messageIDStr returns a stable string identifier for a Pulsar message.
func messageIDStr(msg pulsar.Message) string {
	id := msg.ID()
	if id == nil {
		return ""
	}
	return fmt.Sprintf("%v", id)
}
