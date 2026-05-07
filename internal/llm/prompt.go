package llm

import (
	"bytes"
	"encoding/json"
	"text/template"

	"github.com/samber/oops"
)

// RenderPrompt expands the (system, user) prompt templates against the
// supplied vars using stdlib `text/template`.
//
// Injection-safety strategy
//
// All TemplateVars fields that originate from user-supplied or
// network-supplied data (event payloads, traits, Kapa output) are JSON
// snippets prepared by the caller — i.e. they are already a quoted JSON
// string with control characters escaped. We pass them to text/template as
// opaque strings, which by itself does NOT prevent a hostile field value
// from containing characters that the *consumer* of the rendered text
// (the LLM) might interpret as instructions.
//
// Two defenses are applied:
//
//  1. The template DSL is `text/template`, NOT `html/template`. The
//     auto-escaping of html/template would corrupt JSON values; we instead
//     re-validate every input is well-formed JSON (or empty) before
//     rendering. A malformed payload is rejected at the boundary rather
//     than silently rendered.
//  2. String fields that are NOT JSON (Persona, AnonymousID, UserID) are
//     passed through `template.JSEscapeString` to neutralise quote/newline
//     break-outs. They are short identifiers in practice, so escaping is
//     cheap and prevents a malicious id like `"\nIgnore prior` from
//     reaching the LLM verbatim.
//
// This is a defense-in-depth posture for a hackathon — not a complete
// prompt-injection mitigation. Production systems should additionally
// constrain the LLM via system-message instructions and content filters.
func RenderPrompt(systemTmpl, userTmpl string, vars TemplateVars) (system, user string, err error) {
	if err := validateJSONFields(vars); err != nil {
		return "", "", err
	}

	safe := struct {
		Persona            string
		AnonymousID        string
		UserID             string
		WindowSnapshotJSON string
		FullEventsJSON     string
		TraitsJSON         string
		KapaResultsJSON    string
		LastErrorEventJSON string
		RealtorRosterJSON  string
	}{
		Persona:            template.JSEscapeString(vars.Persona),
		AnonymousID:        template.JSEscapeString(vars.AnonymousID),
		UserID:             template.JSEscapeString(vars.UserID),
		WindowSnapshotJSON: vars.WindowSnapshotJSON,
		FullEventsJSON:     vars.FullEventsJSON,
		TraitsJSON:         vars.TraitsJSON,
		KapaResultsJSON:    vars.KapaResultsJSON,
		LastErrorEventJSON: vars.LastErrorEventJSON,
		RealtorRosterJSON:  vars.RealtorRosterJSON,
	}

	system, err = renderOne("system", systemTmpl, safe)
	if err != nil {
		return "", "", oops.Wrapf(err, "render system prompt")
	}
	user, err = renderOne("user", userTmpl, safe)
	if err != nil {
		return "", "", oops.Wrapf(err, "render user prompt")
	}
	return system, user, nil
}

func renderOne(name, tmplBody string, data any) (string, error) {
	if tmplBody == "" {
		return "", nil
	}
	t, err := template.New(name).Option("missingkey=zero").Parse(tmplBody)
	if err != nil {
		return "", oops.Wrapf(err, "parse %s template", name)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", oops.Wrapf(err, "execute %s template", name)
	}
	return buf.String(), nil
}

// validateJSONFields ensures every *_JSON field on TemplateVars is either
// empty or well-formed JSON. A malformed payload would not break the
// template engine but could mislead the LLM; we reject it at the boundary.
func validateJSONFields(vars TemplateVars) error {
	pairs := map[string]string{
		"WindowSnapshotJSON": vars.WindowSnapshotJSON,
		"FullEventsJSON":     vars.FullEventsJSON,
		"TraitsJSON":         vars.TraitsJSON,
		"KapaResultsJSON":    vars.KapaResultsJSON,
		"LastErrorEventJSON": vars.LastErrorEventJSON,
		"RealtorRosterJSON":  vars.RealtorRosterJSON,
	}
	for name, payload := range pairs {
		if payload == "" {
			continue
		}
		var probe any
		if err := json.Unmarshal([]byte(payload), &probe); err != nil {
			return oops.With("field", name).Wrapf(err, "TemplateVars: not valid JSON")
		}
	}
	return nil
}
