// Package dispatch routes a fired trigger's ActionPayload to a destination
// backend (Slack webhook, mock-email writer, etc.) per §3.8 of the design.
//
// Backends implement a common interface and are registered with the Dispatcher
// keyed by destination prefix (e.g. "slack", "email"). Routing parses the
// destination string "<scheme>:<target>" and selects the matching backend.
//
// The dispatcher is stateless beyond the backend map; concurrent calls are
// safe as long as backends are themselves safe (Slack and Email backends here
// are stateless aside from injected dependencies).
package dispatch

import (
	"context"
	"strings"

	"github.com/samber/oops"
)

// ActionPayload is the contract a fired LLM action must satisfy to be
// dispatched. WP-D's `llm.ActionResult` will adapt to this interface in WP-F's
// wiring step; we deliberately do not import internal/llm to avoid blocking
// on WP-D.
type ActionPayload interface {
	// Template returns the action template name (e.g. "realestate_realtor_pitch").
	Template() string
	// Parsed returns the parsed JSON shape produced by the LLM.
	Parsed() map[string]any
	// Raw returns the raw JSON string (kept for audit/debug).
	Raw() string
}

// Backend is the contract every destination implementation must satisfy.
//
// Implementations MUST honour the supplied context for cancellation/deadline,
// MUST return one of the canonical statuses ("sent" | "failed"), and MUST
// wrap errors via oops so call-site context (rule, anonID) can be added.
type Backend interface {
	// Dispatch delivers the payload. status reports the terminal dispatch
	// state; finalURL is a backend-specific deep-link or identifier that the
	// UI can use to surface the dispatched artifact (e.g. mock email row id).
	Dispatch(ctx context.Context, persona string, payload ActionPayload) (status string, finalURL string, err error)
}

// Dispatcher routes payloads to the appropriate Backend by destination prefix.
type Dispatcher struct {
	backends map[string]Backend
}

// New returns an empty Dispatcher. Register backends via Register before use.
func New() *Dispatcher {
	return &Dispatcher{backends: make(map[string]Backend)}
}

// Register associates a backend with a destination scheme (e.g. "slack").
// Calling Register twice with the same scheme replaces the prior backend.
func (d *Dispatcher) Register(scheme string, b Backend) {
	if scheme == "" || b == nil {
		return
	}
	d.backends[scheme] = b
}

// Dispatch parses the destination string ("<scheme>:<target>"), selects the
// registered backend for the scheme, and forwards the payload. anonID and
// ruleName are passed for error-context only — backends don't see them.
func (d *Dispatcher) Dispatch(
	ctx context.Context,
	destination string,
	payload ActionPayload,
	persona, anonID, ruleName string,
) (status, finalURL string, err error) {
	scheme, _, ok := splitDestination(destination)
	if !ok {
		return "failed", "", oops.
			With("anon_id", anonID).
			With("rule", ruleName).
			With("destination", destination).
			Errorf("dispatch: malformed destination %q (expected <scheme>:<target>)", destination)
	}
	b, ok := d.backends[scheme]
	if !ok {
		return "failed", "", oops.
			With("anon_id", anonID).
			With("rule", ruleName).
			With("destination", destination).
			Errorf("dispatch: no backend registered for scheme %q", scheme)
	}
	if payload == nil {
		return "failed", "", oops.
			With("anon_id", anonID).
			With("rule", ruleName).
			Errorf("dispatch: nil payload")
	}
	status, finalURL, err = b.Dispatch(ctx, persona, payload)
	if err != nil {
		err = oops.
			With("anon_id", anonID).
			With("rule", ruleName).
			With("destination", destination).
			Wrap(err)
	}
	return status, finalURL, err
}

// splitDestination parses "<scheme>:<target>" -> (scheme, target, true).
// Returns false for empty or schemeless strings.
func splitDestination(d string) (scheme, target string, ok bool) {
	idx := strings.IndexByte(d, ':')
	if idx <= 0 || idx == len(d)-1 {
		return "", "", false
	}
	return d[:idx], d[idx+1:], true
}
