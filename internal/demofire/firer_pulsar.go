package demofire

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/apache/pulsar-client-go/pulsar"
	"github.com/samber/oops"

	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/event"
)

// PulsarProducerIface is the subset of pulsar.Producer used by PulsarFirer.
// It is exported so that test code in the _test package can implement it
// without importing the internal pulsarProducer alias.
// Satisfied by both the real pulsar.Producer and test fakes.
type PulsarProducerIface interface {
	Send(context.Context, *pulsar.ProducerMessage) (pulsar.MessageID, error)
	Flush() error
	Close()
}

// pulsarClientIface is the subset of pulsar.Client used by PulsarFirer.
// Narrower interface avoids forcing test fakes to implement every Client method.
type pulsarClientIface interface {
	CreateProducer(pulsar.ProducerOptions) (pulsar.Producer, error)
	Close()
}

// pulsarProducer is the internal type alias used within the package.
type pulsarProducer = PulsarProducerIface

// PulsarFirerConfig holds all configuration for the Pulsar publish path.
//
//   - URL:                  Pulsar broker URL, e.g. "pulsar+ssl://host:6651"
//   - Topic:                Fully-qualified topic name
//   - Token:                JWT token for bearer auth; empty disables auth
//   - TLSTrustCertsFile:    Path to CA cert bundle; used when URL is pulsar+ssl://
//   - TLSValidateHostname:  Whether to validate the broker hostname (default true)
//   - WriteKey:             Placed in the message Properties["writeKey"]
//   - SourceID:             Placed in the message Properties["sourceId"]; defaults to WriteKey
type PulsarFirerConfig struct {
	URL                string
	Topic              string
	Token              string
	TLSTrustCertsFile  string
	TLSValidateHostname bool
	WriteKey           string
	SourceID           string
}

// PulsarFirer publishes persona script steps directly to a Pulsar topic,
// matching the wire format that ingestion-svc uses (key = anonymousId,
// properties = writeKey / sourceId / messageId, KeyBasedBatchBuilder).
type PulsarFirer struct {
	cfg    PulsarFirerConfig
	Logger *slog.Logger
	// Sleep is the ctx-aware sleep injection point; defaults to time.Sleep.
	Sleep func(time.Duration)

	// newClient and newProducer are injected by tests to avoid a real broker.
	newClient   func(pulsar.ClientOptions) (pulsarClientIface, error)
	newProducer func(pulsarClientIface, pulsar.ProducerOptions) (PulsarProducerIface, error)
}

// NewPulsarFirer constructs a PulsarFirer with sensible defaults.
func NewPulsarFirer(cfg PulsarFirerConfig) *PulsarFirer {
	if cfg.SourceID == "" {
		cfg.SourceID = cfg.WriteKey
	}
	return &PulsarFirer{
		cfg:    cfg,
		Logger: slog.Default(),
		Sleep:  time.Sleep,
		newClient: func(opts pulsar.ClientOptions) (pulsarClientIface, error) {
			return pulsar.NewClient(opts)
		},
		newProducer: func(c pulsarClientIface, opts pulsar.ProducerOptions) (PulsarProducerIface, error) {
			return c.CreateProducer(opts)
		},
	}
}

// Fire walks the script, sleeping per DelayMs, and publishes each event as a
// JSON-encoded ProducerMessage to the configured Pulsar topic. Returns the
// number of messages successfully sent.
//
// Resource cleanup: producer is Flushed then Closed; client is Closed after.
// The order is producer.Flush → producer.Close → client.Close.
func (pf *PulsarFirer) Fire(ctx context.Context, script []ScriptStep) (int, error) {
	if err := pf.validate(); err != nil {
		return 0, err
	}

	client, err := pf.newClient(pf.buildClientOptions())
	if err != nil {
		return 0, oops.Wrapf(err, "PulsarFirer.Fire: create client")
	}
	// client.Close must run last, after the producer is fully closed.
	defer client.Close()

	producer, err := pf.newProducer(client, pf.buildProducerOptions())
	if err != nil {
		return 0, oops.Wrapf(err, "PulsarFirer.Fire: create producer")
	}
	defer func() {
		if flushErr := producer.Flush(); flushErr != nil {
			pf.Logger.Warn("PulsarFirer: flush error on close", "err", flushErr)
		}
		producer.Close()
	}()

	sleep := pf.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	log := pf.Logger
	if log == nil {
		log = slog.Default()
	}

	var sent int
	for i, step := range script {
		if err := ctx.Err(); err != nil {
			return sent, oops.Wrapf(err, "PulsarFirer.Fire: cancelled before step %d", i)
		}
		if step.DelayMs > 0 {
			if err := sleepWithCtx(ctx, sleep, time.Duration(step.DelayMs)*time.Millisecond); err != nil {
				return sent, oops.Wrapf(err, "PulsarFirer.Fire: cancelled during delay before step %d", i)
			}
		}

		payload, err := marshalEvent(step.Event)
		if err != nil {
			return sent, oops.With("step", i).Wrapf(err, "PulsarFirer.Fire: marshal event")
		}

		msg := &pulsar.ProducerMessage{
			Payload: payload,
			Key:     step.Event.AnonymousID,
			Properties: map[string]string{
				"writeKey":  pf.cfg.WriteKey,
				"sourceId":  pf.cfg.SourceID,
				"messageId": step.Event.MessageID,
			},
		}

		if _, err := producer.Send(ctx, msg); err != nil {
			return sent, oops.With("step", i).Wrapf(err, "PulsarFirer.Fire: send")
		}

		sent++
		ev := step.Event
		log.Info("demo-fire: step sent",
			"step", i,
			"type", ev.Type,
			"event", ev.Event,
			"path", ev.PagePath(),
		)
	}
	return sent, nil
}

// validate returns an error if required config fields are missing.
func (pf *PulsarFirer) validate() error {
	if pf.cfg.URL == "" {
		return oops.Errorf("PulsarFirer: URL is required (set PULSAR_URL)")
	}
	if pf.cfg.Topic == "" {
		return oops.Errorf("PulsarFirer: Topic is required (set PULSAR_TOPIC)")
	}
	if pf.cfg.WriteKey == "" {
		return oops.Errorf("PulsarFirer: WriteKey is required")
	}
	return nil
}

// buildClientOptions assembles pulsar.ClientOptions from config. TLS options
// are applied only when the URL scheme is pulsar+ssl://.
func (pf *PulsarFirer) buildClientOptions() pulsar.ClientOptions {
	opts := pulsar.ClientOptions{
		URL:              pf.cfg.URL,
		OperationTimeout: 30 * time.Second,
	}

	if pf.cfg.Token != "" {
		opts.Authentication = pulsar.NewAuthenticationToken(pf.cfg.Token)
	}

	if strings.HasPrefix(pf.cfg.URL, "pulsar+ssl://") {
		if pf.cfg.TLSTrustCertsFile != "" {
			opts.TLSTrustCertsFilePath = pf.cfg.TLSTrustCertsFile
		}
		opts.TLSValidateHostname = pf.cfg.TLSValidateHostname
	}

	return opts
}

// buildProducerOptions configures KeyBasedBatchBuilder to match ingestion-svc's
// partition-affinity behaviour.
func (pf *PulsarFirer) buildProducerOptions() pulsar.ProducerOptions {
	return pulsar.ProducerOptions{
		Topic:              pf.cfg.Topic,
		BatcherBuilderType: pulsar.KeyBasedBatchBuilder,
	}
}

// marshalEvent JSON-encodes a single event.Event. We do not wrap it in a
// {batch:[…]} envelope here — Pulsar messages carry one event per message,
// mirroring how ingestion-svc publishes after unwrapping the HTTP batch.
func marshalEvent(e event.Event) ([]byte, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("marshal event: %w", err)
	}
	return b, nil
}

// PulsarClientIface is the exported form of pulsarClientIface so test helpers
// in _test packages can implement it without depending on the full pulsar.Client.
type PulsarClientIface interface {
	CreateProducer(pulsar.ProducerOptions) (pulsar.Producer, error)
	Close()
}

// InjectPulsarFactories replaces the internal client/producer factories on f.
// Intended for use in _test packages only. Both arguments are required.
func InjectPulsarFactories(
	f *PulsarFirer,
	clientFn func(pulsar.ClientOptions) (PulsarClientIface, error),
	producerFn func(PulsarClientIface, pulsar.ProducerOptions) (PulsarProducerIface, error),
) {
	f.newClient = func(opts pulsar.ClientOptions) (pulsarClientIface, error) {
		return clientFn(opts)
	}
	f.newProducer = func(c pulsarClientIface, opts pulsar.ProducerOptions) (PulsarProducerIface, error) {
		return producerFn(c.(PulsarClientIface), opts)
	}
}
