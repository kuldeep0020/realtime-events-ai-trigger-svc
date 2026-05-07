package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/sse"
)

// fireScriptRequest carries the persona to fire. Body schema is small so
// we accept it via either JSON body or query string for ergonomics.
type fireScriptRequest struct {
	Persona string `json:"persona"`
}

// handleFireScript invokes the configured FireScript callback for the
// requested persona. Returns 501 when no callback is wired (stage 1 stub).
//
// The handler runs Fire in a fresh context derived from a server-side
// timeout — NOT r.Context() — because the demo script outlives the HTTP
// request (browser disconnects when the user moves to another tab). We
// deliberately let the script complete server-side so the dashboard SSE
// streams pick up every event.
func (s *Server) handleFireScript(w http.ResponseWriter, r *http.Request) {
	if s.fireScript == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]any{
			"error":  "demo fire-script not yet wired",
			"status": "stub",
			"todo":   "wire FireScript via api.Config",
		})
		return
	}

	persona := strings.TrimSpace(r.URL.Query().Get("persona"))
	if persona == "" {
		// Accept JSON body as fallback.
		var req fireScriptRequest
		if r.Body != nil && r.ContentLength != 0 {
			if !decodeJSON(w, r, &req) {
				return
			}
			persona = strings.TrimSpace(req.Persona)
		}
	}
	if persona == "" {
		writeError(w, http.StatusBadRequest, "persona is required (query string or JSON body)")
		return
	}
	persona = strings.ToLower(persona)
	if persona == "rsself" || persona == "rs_self" {
		persona = "rs-self"
	}
	if persona != "realestate" && persona != "rs-self" {
		writeError(w, http.StatusBadRequest, "unsupported persona: "+persona)
		return
	}

	// Detached context: the demo script is ~32 seconds; we don't want a
	// browser disconnect to abort it. We respect a max wall-clock cap so
	// runaway invocations don't leak goroutines.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	count, err := s.fireScript(ctx, persona)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "fire-script failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"persona":     persona,
		"event_count": count,
		"status":      "fired",
	})
}

// handleDemoReset clears demo state. Truncates triggers + events for a
// fresh demo, but keeps configs / canned_responses / mock_profiles intact
// so the rules engine still has a runtime config.
//
// Tested only against the logical SQL — the integration test path runs
// without a live DB and short-circuits.
func (s *Server) handleDemoReset(w http.ResponseWriter, r *http.Request) {
	if s.pool == nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "begin tx: "+err.Error())
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Order matters: mock_emails references triggers, so wipe it first.
	stmts := []string{
		`DELETE FROM mock_emails`,
		`DELETE FROM triggers`,
		`DELETE FROM cooldowns`,
		`DELETE FROM events`,
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			writeError(w, http.StatusInternalServerError, "reset failed: "+err.Error())
			return
		}
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "commit: "+err.Error())
		return
	}

	resp := map[string]any{"status": "reset"}
	if s.onDemoReset != nil {
		// Use a fresh context — the DB tx context is already spent.
		resetCtx, resetCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer resetCancel()
		cooldownsCleared, windowsCleared, resetErr := s.onDemoReset(resetCtx)
		if resetErr != nil {
			// Non-fatal: PG state is already clean. Log and surface in response.
			resp["in_memory_reset_error"] = resetErr.Error()
		} else {
			resp["cooldowns_cleared"] = cooldownsCleared
			resp["windows_cleared"] = windowsCleared
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// replayLastTriggerResponse mirrors the JSONB shape we emit on the
// triggers SSE stream so the UI can hot-replay even after a reset.
type replayLastTriggerResponse struct {
	ID             string          `json:"id"`
	RuleName       string          `json:"rule_name"`
	Persona        string          `json:"persona"`
	AnonymousID    string          `json:"anonymous_id"`
	FiredAt        time.Time       `json:"fired_at"`
	WindowSnapshot json.RawMessage `json:"window_snapshot"`
	LLMParsed      json.RawMessage `json:"llm_parsed,omitempty"`
	Destination    string          `json:"destination"`
	DispatchStatus string          `json:"dispatch_status"`
}

// handleReplayLastTrigger returns the most recently fired trigger row.
// Used by the demo controller's "Replay last trigger" button when a fresh
// page render needs to re-show the trigger card.
func (s *Server) handleReplayLastTrigger(w http.ResponseWriter, r *http.Request) {
	if s.pool == nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	const q = `
		SELECT id, rule_name, persona, anonymous_id, fired_at,
		       window_snapshot, llm_parsed, destination, dispatch_status
		FROM triggers
		ORDER BY fired_at DESC
		LIMIT 1`

	var (
		out          replayLastTriggerResponse
		windowSnap   []byte
		llmParsed    []byte
	)
	err := s.pool.QueryRow(ctx, q).Scan(
		&out.ID, &out.RuleName, &out.Persona, &out.AnonymousID, &out.FiredAt,
		&windowSnap, &llmParsed, &out.Destination, &out.DispatchStatus,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "no triggers yet")
			return
		}
		writeError(w, http.StatusInternalServerError, "query failed: "+err.Error())
		return
	}
	out.WindowSnapshot = windowSnap
	if len(llmParsed) > 0 {
		out.LLMParsed = llmParsed
	}

	// Publish to the SSE triggers stream so any subscribed dashboard tab
	// re-renders the trigger card immediately — without this the "Replay last
	// trigger" button returns 200 but the Triggers Fired column stays empty.
	s.hub.Publish(sse.StreamTriggers, replayTriggerSSEMessage(out))

	writeJSON(w, http.StatusOK, out)
}

// replayTriggerSSEMessage builds the sse.Message for a replayed trigger row,
// matching the shape emitted by runtime_dispatch.go fireMatch so the
// frontend TriggerCard component renders it identically.
//
// Extracted as a pure function so it can be unit-tested without a Postgres
// pool or a running hub.
func replayTriggerSSEMessage(row replayLastTriggerResponse) sse.Message {
	return sse.Message{
		Event: sse.StreamTriggers,
		Data: map[string]any{
			"id":              row.ID,
			"rule_name":       row.RuleName,
			"persona":         row.Persona,
			"anonymous_id":    row.AnonymousID,
			"fired_at":        row.FiredAt.UTC().Format(time.RFC3339),
			"window_snapshot": json.RawMessage(row.WindowSnapshot),
			"destination":     row.Destination,
			"dispatch_status": row.DispatchStatus,
			"llm_parsed":      json.RawMessage(row.LLMParsed),
		},
	}
}

// handleListMockEmails returns the most recent mock_emails rows. Optional
// `?to=` filter to scope to a recipient.
func (s *Server) handleListMockEmails(w http.ResponseWriter, r *http.Request) {
	if s.pool == nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	to := r.URL.Query().Get("to")
	const limit = 100

	var (
		rows pgx.Rows
		err  error
	)
	if to != "" {
		rows, err = s.pool.Query(ctx, `
			SELECT id, trigger_id, to_email, subject, body_markdown, links, created_at
			FROM mock_emails WHERE to_email = $1
			ORDER BY created_at DESC LIMIT $2`, to, limit)
	} else {
		rows, err = s.pool.Query(ctx, `
			SELECT id, trigger_id, to_email, subject, body_markdown, links, created_at
			FROM mock_emails
			ORDER BY created_at DESC LIMIT $1`, limit)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed: "+err.Error())
		return
	}
	defer rows.Close()

	type emailRow struct {
		ID           string          `json:"id"`
		TriggerID    *string         `json:"trigger_id,omitempty"`
		ToEmail      string          `json:"to_email"`
		Subject      string          `json:"subject"`
		BodyMarkdown string          `json:"body_markdown"`
		Links        json.RawMessage `json:"links,omitempty"`
		CreatedAt    time.Time       `json:"created_at"`
	}
	out := make([]emailRow, 0, 16)
	for rows.Next() {
		var e emailRow
		var triggerIDStr *string
		var links []byte
		if err := rows.Scan(&e.ID, &triggerIDStr, &e.ToEmail, &e.Subject,
			&e.BodyMarkdown, &links, &e.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed: "+err.Error())
			return
		}
		e.TriggerID = triggerIDStr
		if len(links) > 0 {
			e.Links = links
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "iterate failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"emails": out})
}
