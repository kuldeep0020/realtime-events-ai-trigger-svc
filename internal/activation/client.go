// Package activation provides the RudderStack Activation API client used by
// the enricher to fetch profile traits before LLM action generation.
//
// Two implementations are exported:
//
//   - MockClient — backed by Postgres `mock_profiles`; used for the hackathon
//     demo (ACTIVATION_MODE=mock). Implements the same identity-resolution
//     fallback behavior described in §3.5: if a `user_id` lookup misses, retry
//     with `anonymous_id` of the same value.
//   - LiveClient — POSTs to the real Activation API at
//     `{ACTIVATION_BASE_URL}/activation` with a Bearer SAT. Stubbed for a
//     one-line production swap; never exercised in the demo path.
//
// The wire shape (request + response) matches the official v1 spec at
// `docs/profiles/dev-docs/activation-api/v1`. See §3.5 of the design doc for
// the canonical reference.
package activation

import "context"

// Client is the abstraction the enricher consumes. Both Mock and Live
// implementations honour this contract.
type Client interface {
	GetProfile(ctx context.Context, req ProfileRequest) (ProfileResponse, error)
}

// ProfileRequest mirrors the official `POST /v1/activation` body.
//
//	{
//	  "entity":        "user",
//	  "destinationId": "<redis_destination_id>",
//	  "id":            { "type": "anonymous_id", "value": "anon_demo-re-001" }
//	}
type ProfileRequest struct {
	Entity        string `json:"entity"`        // "user" | "account" | "project" | ...
	DestinationID string `json:"destinationId"` // workspace-configured Redis destination ID
	ID            ID     `json:"id"`
}

// ID is the (type, value) pair that names a profile.
//
// Identifier types are project-defined in `pb_project.yaml > entities >
// id_types`. The hackathon personas use `anonymous_id` and `user_id`;
// production may also see `email`, `main_id`, `salesforce_id`, etc.
type ID struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// ProfileResponse mirrors the official response body.
//
// On a hit:
//
//	{
//	  "entity": "user",
//	  "id":     { "type": "anonymous_id", "value": "anon_demo-re-001" },
//	  "data":   { /* traits map */ }
//	}
//
// On a miss, the API returns 200 with the same shape and `"data": {}`. The
// MockClient mirrors this contract and never returns nil for Data — callers
// can range over Data without nil-checking.
type ProfileResponse struct {
	Entity string         `json:"entity"`
	ID     ID             `json:"id"`
	Data   map[string]any `json:"data"`
}
