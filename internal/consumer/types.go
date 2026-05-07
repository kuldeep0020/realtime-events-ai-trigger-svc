// Package consumer provides the Pulsar consumer that reads source events,
// deserializes them into the canonical event type, and forwards them on a
// channel for downstream processing.
package consumer

import (
	"time"

	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/event"
)

// ProcessedEvent is the output type of the consumer.Run loop. It carries the
// deserialized event plus metadata extracted from Pulsar message properties.
type ProcessedEvent struct {
	// PulsarMessageID is the string representation of the Pulsar message ID,
	// used for deduplication and audit logging.
	PulsarMessageID string

	// WriteKey is the RudderStack workspace write key extracted from Pulsar
	// message properties. Empty string means the property was absent.
	WriteKey string

	// SourceID is the RudderStack source ID extracted from Pulsar message
	// properties. Empty string means the property was absent.
	SourceID string

	// Event is the parsed canonical event. Never nil in a successfully
	// processed message.
	Event *event.Event

	// ReceivedAt is the wall-clock time the message was dequeued from Pulsar.
	ReceivedAt time.Time
}
