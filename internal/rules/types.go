// Package rules implements the trigger rules DSL evaluator (§3.4 of the design).
//
// A Rule is a named, persona-scoped predicate over a window.Snapshot. The
// rules engine evaluates rules either on each incoming event (consumer path)
// or on each idle ticker (for time-based predicates). Rules are loaded from
// Postgres at startup and hot-reloaded periodically; loads are atomic via a
// pointer swap so evaluation never sees a torn rule list.
//
// Cooldown is the LAST gate before a Match is reported — predicate
// evaluation always runs, then cooldown filters the result. This keeps
// cooldown semantics consistent regardless of which evaluator path fired.
package rules

import (
	"time"

	"github.com/google/uuid"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/window"
)

// Rule is a parsed, ready-to-evaluate trigger rule.
type Rule struct {
	ID       uuid.UUID
	Name     string
	Persona  string
	When     Expr
	Fire     FireSpec
	Cooldown time.Duration
	Enabled  bool
}

// FireSpec describes what to do when the rule's predicate matches.
type FireSpec struct {
	ActionTemplate string
	Destination    string // "slack:<channel>" or "email:<target>"
	CooldownSecs   int
}

// Match is the result of a successful rule evaluation. It carries the
// snapshot used for the decision so downstream consumers (LLM enricher,
// dispatcher) have a consistent view.
type Match struct {
	RuleID    uuid.UUID
	RuleName  string
	Persona   string
	FiredAt   time.Time
	Anonymous string
	Fire      FireSpec
	Snapshot  window.Snapshot
}

// CooldownGate is the contract the engine uses to decide whether a Match
// should be emitted. Implementations can be DB-backed (the production case)
// or in-memory (tests). Implementations MUST be safe to call concurrently.
type CooldownGate interface {
	// Allow returns true if (anonID, ruleName) is NOT currently on cooldown.
	// Allow is invoked before Mark and is the LAST gate before emission.
	Allow(anonID, ruleName string, now time.Time) bool
	// Mark records the fire-time of (anonID, ruleName). Implementations must
	// be idempotent for the same (anonID, ruleName, now) tuple.
	Mark(anonID, ruleName string, now time.Time, cooldown time.Duration)
}
