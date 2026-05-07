// Package db provides the Postgres connection pool, migration runner, and typed
// query helpers for the realtime-ai-trigger service.
package db

import (
	"time"

	"github.com/google/uuid"
)

// EventRow is the materialized shape of a row from the events table.
type EventRow struct {
	ID               int64
	PulsarMessageID  string
	AnonymousID      string
	UserID           string
	WriteKey         string
	EventType        string
	EventName        string
	PagePath         string
	Payload          []byte
	ReceivedAt       time.Time
}

// RuleRow is the materialized shape of a joined rules + config row used by
// the rule evaluator. Callers may extend by reading rule.spec directly.
type RuleRow struct {
	ID       uuid.UUID
	Name     string
	Spec     []byte // raw JSONB
	Persona  string // denormalized from configs.persona
	ConfigID uuid.UUID
}

// TriggerRow is the complete insert shape for the triggers table.
type TriggerRow struct {
	ID             uuid.UUID  // set by caller or zero → DB generates
	RuleID         *uuid.UUID // nullable FK
	RuleName       string
	Persona        string
	AnonymousID    string
	FiredAt        time.Time
	WindowSnapshot []byte // JSONB
	FullEvents     []byte // JSONB
	EnrichedTraits []byte // JSONB, nullable
	KapaResult     []byte // JSONB, nullable
	LLMRaw         string
	LLMParsed      []byte // JSONB, nullable
	LLMSource      string // 'canned' | 'live' | 'fallback'
	Destination    string
	DispatchStatus string // 'pending' | 'sent' | 'failed'
	DispatchedAt   *time.Time
	Error          string
}

// MockEmailRow is the insert shape for mock_emails.
type MockEmailRow struct {
	TriggerID    *uuid.UUID // nullable FK
	ToEmail      string
	Subject      string
	BodyMarkdown string
	Links        []byte // JSONB, nullable
}
