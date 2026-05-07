package dispatch

// rawLLMResult is the inline shape we accept from any package that holds
// a (Template, Parsed, Raw) triple. Callers (notably the serve-mode
// wiring in cmd/realtime-trigger) construct an LLMPayload by passing
// these three fields directly — no llm-package dependency.
type rawLLMResult struct {
	template string
	parsed   map[string]any
	raw      string
}

// LLMPayload adapts an (template, parsed, raw) triple to the
// ActionPayload contract the dispatch backends require. We deliberately
// keep this struct unexported-data + exported-constructor so callers
// can't accidentally mutate the underlying maps after construction.
//
// This adapter is the bridge defined in §13.4 step 3 between WP-D's
// llm.ActionResult and WP-E's dispatch.ActionPayload. The two packages
// were authored independently to avoid a cross-WP dependency; this file
// is the only place the bridge lives.
type LLMPayload struct {
	r rawLLMResult
}

// NewLLMPayload constructs an ActionPayload from explicit fields. The
// caller supplies the template name, the parsed JSON map, and the raw
// JSON string. All three are stored as-is — no defensive copy — so the
// caller MUST treat them as owned-by-the-payload after this call.
func NewLLMPayload(template string, parsed map[string]any, raw string) ActionPayload {
	return &LLMPayload{r: rawLLMResult{
		template: template,
		parsed:   parsed,
		raw:      raw,
	}}
}

// Template returns the action template name (e.g. "realestate_realtor_pitch").
func (p *LLMPayload) Template() string {
	if p == nil {
		return ""
	}
	return p.r.template
}

// Parsed returns the parsed JSON shape produced by the LLM.
func (p *LLMPayload) Parsed() map[string]any {
	if p == nil {
		return nil
	}
	return p.r.parsed
}

// Raw returns the raw JSON string (kept for audit/debug).
func (p *LLMPayload) Raw() string {
	if p == nil {
		return ""
	}
	return p.r.raw
}
