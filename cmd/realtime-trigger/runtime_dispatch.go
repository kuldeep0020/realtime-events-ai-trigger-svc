package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/activation"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/db"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/dispatch"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/llm"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/rules"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/sse"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/window"
)

// fireMatch is invoked once per matched rule. It builds the LLM context,
// runs the LLM, dispatches, and writes the trigger row + SSE.
//
// Errors at intermediate stages degrade gracefully: a missing profile or
// kapa lookup logs a warning and proceeds with an empty placeholder.
func (rt *runtime) fireMatch(ctx context.Context, m rules.Match, persona string) {
	// Guard: if essential components are absent (unit-test stubs) bail out
	// cleanly rather than panicking on nil dereferences downstream.
	if rt.llmClient == nil {
		return
	}

	// Cooldown is also gated by the engine itself; PG cooldown table is a
	// best-effort durability layer for cross-restart durability. Skip the PG
	// check when no pool is available (tests, degraded mode).
	if rt.pool != nil {
		if cooled, _ := db.IsCooledDown(ctx, rt.pool, m.RuleID, m.Anonymous); cooled {
			// Surface the override so operators can tell when the in-memory gate
			// was cleared (e.g. after a pod restart) but PG still holds a live
			// cooldown row. Without this log line the suppression is invisible.
			rt.pgCooldownOverrides.Add(1)
			rt.log.Warn("serve: pg_cooldown_overrode_engine_gate",
				"rule", m.RuleName,
				"anonymous_id", m.Anonymous,
				"reason", "pg_cooldown_overrode_engine_gate",
				"total_overrides", rt.pgCooldownOverrides.Load(),
			)
			return
		}
	}

	tc := rt.buildTriggerContext(ctx, m, persona)
	result, err := rt.llmClient.Generate(ctx, m.Fire.ActionTemplate, tc.vars)
	if err != nil {
		rt.log.Warn("serve: llm generate failed", "err", err)
		return
	}

	payload := dispatch.NewLLMPayload(result.Template, result.Parsed, result.Raw)
	dispatchStatus, _, dispatchErr := rt.dispatcher.Dispatch(ctx,
		m.Fire.Destination, payload, persona, m.Anonymous, m.RuleName,
	)
	if dispatchErr != nil {
		rt.log.Warn("serve: dispatch failed", "err", dispatchErr,
			"destination", m.Fire.Destination)
	}

	now := time.Now().UTC()
	ruleID := m.RuleID
	row := db.TriggerRow{
		RuleID:         &ruleID,
		RuleName:       m.RuleName,
		Persona:        persona,
		AnonymousID:    m.Anonymous,
		FiredAt:        m.FiredAt,
		WindowSnapshot: tc.snapJSON,
		FullEvents:     tc.fullEventsJSON,
		EnrichedTraits: tc.traitsJSON,
		KapaResult:     tc.kapaJSON,
		LLMRaw:         result.Raw,
		// LLMParsed accepts JSONB bytes; result.Raw is the canonical JSON
		// representation of result.Parsed (canned mode echoes raw_json).
		LLMParsed:      []byte(result.Raw),
		LLMSource:      result.Source,
		Destination:    m.Fire.Destination,
		DispatchStatus: dispatchStatus,
		DispatchedAt:   &now,
	}
	if dispatchErr != nil {
		row.Error = dispatchErr.Error()
	}
	triggerID, err := db.InsertTrigger(ctx, rt.pool, row)
	if err != nil {
		rt.log.Warn("serve: insert trigger failed", "err", err)
	}

	if cooldown := time.Duration(m.Fire.CooldownSecs) * time.Second; cooldown > 0 {
		_ = db.UpsertCooldown(ctx, rt.pool, m.RuleID, m.Anonymous, now.Add(cooldown))
	}

	rt.hub.Publish(sse.StreamTriggers, sse.Message{
		Event: sse.StreamTriggers,
		Data: map[string]any{
			"id":              triggerID.String(),
			"rule_name":       m.RuleName,
			"persona":         persona,
			"anonymous_id":    m.Anonymous,
			"fired_at":        m.FiredAt,
			"destination":     m.Fire.Destination,
			"dispatch_status": dispatchStatus,
			"llm_parsed":      result.Parsed,
		},
	})
	if scheme, _, _ := splitDest(m.Fire.Destination); scheme == "email" {
		rt.hub.Publish(sse.StreamMockEmails, sse.Message{
			Event: sse.StreamMockEmails,
			Data: map[string]any{
				"trigger_id": triggerID.String(),
				"persona":    persona,
				"subject":    stringFromMap(result.Parsed, "subject"),
				"body_md":    stringFromMap(result.Parsed, "body_markdown"),
			},
		})
	}
}

// triggerContext bundles the four enrichment artefacts used by both the
// LLM call and the eventual trigger-row insert. We pre-build them once
// per fireMatch so the JSON marshalling cost is paid only once.
type triggerContext struct {
	vars           llm.TemplateVars
	snapJSON       []byte
	fullEventsJSON []byte
	traitsJSON     []byte
	kapaJSON       []byte
}

// buildTriggerContext fetches the full event log, profile traits, and
// (for rs-self) Kapa retrieval result. Errors are logged but never fatal —
// a missing component degrades to an empty payload section.
func (rt *runtime) buildTriggerContext(ctx context.Context, m rules.Match, persona string) triggerContext {
	since := time.Now().Add(-15 * time.Minute)
	var evts []db.EventRow
	if rt.pool != nil {
		var err error
		evts, err = db.FetchEventsForAnon(ctx, rt.pool, m.Anonymous, since)
		if err != nil {
			rt.log.Warn("serve: fetch events failed", "err", err, "anon", m.Anonymous)
		}
	}
	fullEventsJSON, _ := json.Marshal(eventsToWire(evts))

	idType := "anonymous_id"
	idValue := m.Anonymous
	if m.Snapshot.UserID != "" {
		idType = "user_id"
		idValue = m.Snapshot.UserID
	}
	prof, err := rt.activationClient.GetProfile(ctx, activation.ProfileRequest{
		Entity:        "user",
		DestinationID: rt.cfg.ActivationDestID,
		ID:            activation.ID{Type: idType, Value: idValue},
	})
	if err != nil {
		rt.log.Warn("serve: activation lookup failed", "err", err)
	}
	traitsJSON, _ := json.Marshal(prof.Data)

	var kapaJSON []byte
	if persona == llm.PersonaRSSelf {
		query := buildKapaQuery(m.Snapshot)
		if query != "" {
			res, err := rt.kapaClient.Retrieve(ctx, query)
			if err != nil {
				rt.log.Warn("serve: kapa retrieve failed", "err", err)
			} else {
				kapaJSON, _ = json.Marshal(res)
			}
		}
	}

	snapJSON, _ := json.Marshal(m.Snapshot)
	lastErrJSON, _ := json.Marshal(m.Snapshot.LastErrorEvent)

	return triggerContext{
		vars: llm.TemplateVars{
			Persona:            persona,
			AnonymousID:        m.Anonymous,
			UserID:             m.Snapshot.UserID,
			WindowSnapshotJSON: string(snapJSON),
			FullEventsJSON:     string(fullEventsJSON),
			TraitsJSON:         string(traitsJSON),
			KapaResultsJSON:    string(kapaJSON),
			LastErrorEventJSON: string(lastErrJSON),
		},
		snapJSON:       snapJSON,
		fullEventsJSON: fullEventsJSON,
		traitsJSON:     traitsJSON,
		kapaJSON:       kapaJSON,
	}
}

// splitDest re-exports the dispatch package's destination-split logic.
// Re-implemented here to avoid exporting a package-internal helper.
func splitDest(d string) (scheme, target string, ok bool) {
	idx := strings.IndexByte(d, ':')
	if idx <= 0 || idx == len(d)-1 {
		return "", "", false
	}
	return d[:idx], d[idx+1:], true
}

// buildKapaQuery synthesises the canned-kapa query used by rs-self
// triggers. We anchor on the last error event's error_code so the
// canned-kapa pattern matches.
func buildKapaQuery(snap window.Snapshot) string {
	if snap.LastErrorEvent.EventName == "" {
		return ""
	}
	if code, ok := snap.LastErrorEvent.Properties["error_code"].(string); ok && code != "" {
		return fmt.Sprintf("Amplitude API key error %s", code)
	}
	return snap.LastErrorEvent.EventName
}

// eventsToWire shrinks db.EventRow values to a slice of maps ready for
// JSON serialisation into TemplateVars.FullEventsJSON.
func eventsToWire(rows []db.EventRow) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		out = append(out, map[string]any{
			"id":           r.ID,
			"anonymous_id": r.AnonymousID,
			"user_id":      r.UserID,
			"event_type":   r.EventType,
			"event_name":   r.EventName,
			"page_path":    r.PagePath,
			"received_at":  r.ReceivedAt,
			"payload":      json.RawMessage(r.Payload),
		})
	}
	return out
}

// stringFromMap returns a string entry from a JSON-decoded map, or empty.
func stringFromMap(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}
