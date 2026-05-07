// Package kapa provides the Kapa.ai retrieval client used to enrich rs-self
// triggers with documentation context before LLM action generation.
//
// Two implementations are exported:
//
//   - CannedClient — backed by Postgres `canned_kapa_responses`; default for
//     the demo (KAPA_MODE=canned). Matches by exact `query_pattern` first
//     and falls back to SQL LIKE patterns when the pattern column contains
//     wildcards.
//   - LiveClient — POSTs to the real Kapa.ai API. Used by the seed CLI
//     (KAPA_MODE=live) to capture canned responses; never exercised at
//     trigger time.
//
// Wire shape (per §3.6 of the design doc):
//
//	POST https://api.kapa.ai/query/v1/projects/{project_id}/chat/
//	Headers: X-API-KEY: <key>, Content-Type: application/json
//	Body:    {"query": "..."}
//	Resp:    {"answer": "...", "is_uncertain": false,
//	          "relevant_sources": [{"title": "...", "source_url": "..."}]}
package kapa

import "context"

// Client is the abstraction the enricher consumes; both Canned and Live
// implementations honour this contract.
type Client interface {
	Retrieve(ctx context.Context, query string) (Result, error)
}

// Result mirrors the Kapa response body. The `Source` field is internal
// metadata (not on the Kapa wire) tagging where the result originated:
// "canned" (PG row), "live" (HTTP), or "fallback" (default-canned default).
type Result struct {
	Answer          string   `json:"answer"`
	IsUncertain     bool     `json:"is_uncertain"`
	RelevantSources []Source `json:"relevant_sources"`
	Source          string   `json:"-"` // "canned" | "live" | "fallback"
}

// Source is one citation entry returned alongside an answer.
type Source struct {
	Title     string `json:"title"`
	SourceURL string `json:"source_url"`
}

// defaultCannedResult builds a generic, never-empty fallback so the demo
// always renders something even when the canned table is empty and the live
// API is unavailable.
//
// The fallback is intentionally cautious — `IsUncertain: true` signals the
// downstream LLM (and the UI badge) that the answer is generic. Callers can
// detect a fallback either by `Source == "fallback"` or by inspecting
// IsUncertain.
func defaultCannedResult(query string) Result {
	_ = query // included in the docstring envelope for future template expansion.
	return Result{
		Answer: "I don't have a specific answer for this. Please consult the " +
			"RudderStack docs at https://www.rudderstack.com/docs/ or contact " +
			"support for help.",
		IsUncertain:     true,
		RelevantSources: []Source{},
		Source:          "fallback",
	}
}
