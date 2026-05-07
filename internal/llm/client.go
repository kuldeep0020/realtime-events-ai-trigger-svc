// Package llm provides the action-generation client used to render persona
// triggers into structured JSON payloads (Slack Block Kit, mock-email
// markdown, etc.).
//
// Three implementations are exported:
//
//   - CannedClient — backed by Postgres `canned_responses`; default for the
//     demo (LLM_MODE=canned). Always returns a renderable result by falling
//     back to a hardcoded baseline when no row is seeded.
//   - LocalAgentClient — POSTs to the local agent SSE endpoint (used by the
//     seed CLI when LLM_MODE=live to capture canned responses).
//   - BedrockClient — minimal stub for the documented Bedrock fallback
//     using the presigned-URL pattern (12h-TTL key) from §0. Compile-clean
//     and interface-conforming; live behaviour out of hackathon scope.
//
// All three implementations honour the same Client interface so the wiring
// layer can swap them by config without changing call sites.
package llm

import "context"

// Client is the abstraction the enricher / dispatcher consumes.
type Client interface {
	Generate(ctx context.Context, templateName string, vars TemplateVars) (ActionResult, error)
}

// TemplateVars collects every input the action templates may reference. All
// fields are pre-rendered to strings (typically JSON snippets) so the prompt
// rendering layer never has to format Go values directly.
//
// Why strings (not raw structs)?
//
//   - The seed-time prompt templates were authored against JSON snippets;
//     keeping the type stable preserves prompt fidelity between canned and
//     live modes.
//   - It forces the caller to commit to a serialization at the boundary,
//     which is the right place for size limits and redaction.
//   - It removes any ambiguity about what `text/template` will produce when
//     printing maps or pointers.
type TemplateVars struct {
	Persona            string
	AnonymousID        string
	UserID             string
	WindowSnapshotJSON string // aggregations
	FullEventsJSON     string // raw events from PG, ordered, last 15 min
	TraitsJSON         string // mock activation traits
	KapaResultsJSON    string // present only for rs-self
	LastErrorEventJSON string // present when applicable
	RealtorRosterJSON  string // present only for realestate
}

// ActionResult is the rendered output the dispatcher consumes. Source
// indicates the origin so observability + UI can surface the degraded path.
type ActionResult struct {
	Template       string
	Raw            string
	Parsed         map[string]any
	Source         string // "canned" | "live" | "fallback"
	DegradedReason string
}

// Template names referenced by both the canned tables and the hardcoded
// defaults. Centralising them here prevents drift between seed YAMLs, rule
// configs, and the fallback registry.
const (
	TemplateRealestateRealtorPitch = "realestate_realtor_pitch"
	TemplateRSOnboardingStuck      = "rs_onboarding_stuck"
)

// Personas referenced by the rule configs.
const (
	PersonaRealestate = "realestate"
	PersonaRSSelf     = "rs-self"
)
