package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

// handleFireScript is a 501 stub. The actual demo-firing path lives in
// internal/demofire/ (WP-F). When wired, this handler dispatches the
// persona-specific event sequence to INGESTION_URL/v1/batch.
//
// TODO(WP-F): wire into internal/demofire/ once that package lands.
func (s *Server) handleFireScript(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"error":  "demo fire-script not yet wired",
		"status": "stub",
		"todo":   "WP-F internal/demofire/",
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

	writeJSON(w, http.StatusOK, map[string]any{"status": "reset"})
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
	writeJSON(w, http.StatusOK, out)
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
