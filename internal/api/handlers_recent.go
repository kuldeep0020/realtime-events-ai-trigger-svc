package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// handleRecentEvents returns the most recent N raw event payloads from the
// events table. The payload column stores the full RudderStack v3 SDK event in
// camelCase JSON, so we emit it verbatim inside the envelope.
//
// Default limit=50, max limit=200. Rejects negative values silently (uses default).
func (s *Server) handleRecentEvents(w http.ResponseWriter, r *http.Request) {
	if s.pool == nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}

	limit := parseLimitParam(r, 50, 200)

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	const q = `SELECT payload FROM events ORDER BY received_at DESC LIMIT $1`

	rows, err := s.pool.Query(ctx, q, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed: "+err.Error())
		return
	}
	defer rows.Close()

	events := make([]json.RawMessage, 0, limit)
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed: "+err.Error())
			return
		}
		events = append(events, json.RawMessage(payload))
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "iterate failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

// handleActiveSessions returns all current in-memory window snapshots.
// Windows are not persisted — this endpoint serves dashboard rehydration
// for browser refreshes where SSE state is lost.
//
// Returns 503 when the window store is not wired (nil).
func (s *Server) handleActiveSessions(w http.ResponseWriter, r *http.Request) {
	if s.windowStore == nil {
		writeError(w, http.StatusServiceUnavailable, "window store unavailable")
		return
	}

	snaps := s.windowStore.SnapshotAll()
	writeJSON(w, http.StatusOK, map[string]any{"sessions": snaps})
}

// handleRecentTriggers returns the most recent N trigger rows from the
// triggers table, mapped to the SSETriggerPayload wire-shape so the frontend
// TriggerCard rendering path can consume them identically.
//
// Default limit=20, max limit=100.
func (s *Server) handleRecentTriggers(w http.ResponseWriter, r *http.Request) {
	if s.pool == nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}

	limit := parseLimitParam(r, 20, 100)

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	const q = `
		SELECT id, rule_name, persona, anonymous_id, fired_at,
		       window_snapshot, llm_parsed, destination, dispatch_status
		FROM triggers
		ORDER BY fired_at DESC
		LIMIT $1`

	rows, err := s.pool.Query(ctx, q, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed: "+err.Error())
		return
	}
	defer rows.Close()

	out := make([]replayLastTriggerResponse, 0, limit)
	for rows.Next() {
		var t replayLastTriggerResponse
		var windowSnap, llmParsed []byte
		if err := rows.Scan(
			&t.ID, &t.RuleName, &t.Persona, &t.AnonymousID, &t.FiredAt,
			&windowSnap, &llmParsed, &t.Destination, &t.DispatchStatus,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed: "+err.Error())
			return
		}
		t.WindowSnapshot = windowSnap
		if len(llmParsed) > 0 {
			t.LLMParsed = llmParsed
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "iterate failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"triggers": out})
}

// parseLimitParam reads ?limit= from the query string, clamping to [1, max].
// Returns defaultVal when absent, invalid, zero, or negative.
func parseLimitParam(r *http.Request, defaultVal, maxVal int) int {
	v := r.URL.Query().Get("limit")
	if v == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return defaultVal
	}
	if n > maxVal {
		return maxVal
	}
	return n
}
