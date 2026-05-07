package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// handleAdminSeed invokes the configured AdminSeed callback. Returns 501
// when not wired so existing tests continue to pass.
func (s *Server) handleAdminSeed(w http.ResponseWriter, r *http.Request) {
	if s.adminSeed == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]any{
			"error":  "admin seed not yet wired",
			"status": "stub",
			"todo":   "wire AdminSeed via api.Config",
		})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	if err := s.adminSeed(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "seed failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "seeded"})
}

// handleAdminCanned returns rows from canned_responses filtered by query
// params `template` and `persona` (both optional). Used during rehearsal to
// confirm what the LLM CannedClient will return for a given template.
func (s *Server) handleAdminCanned(w http.ResponseWriter, r *http.Request) {
	if s.pool == nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	template := r.URL.Query().Get("template")
	persona := r.URL.Query().Get("persona")

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	// Build the query depending on which filters were supplied.
	const baseQuery = `
		SELECT id, template_name, persona, variant, raw_json, priority, created_at
		FROM canned_responses
		WHERE ($1 = '' OR template_name = $1)
		  AND ($2 = '' OR persona = $2)
		ORDER BY priority DESC, created_at DESC
		LIMIT 200`

	rows, err := s.pool.Query(ctx, baseQuery, template, persona)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed: "+err.Error())
		return
	}
	defer rows.Close()

	type cannedRow struct {
		ID           string          `json:"id"`
		TemplateName string          `json:"template_name"`
		Persona      string          `json:"persona"`
		Variant      string          `json:"variant"`
		RawJSON      json.RawMessage `json:"raw_json"`
		Priority     int             `json:"priority"`
		CreatedAt    time.Time       `json:"created_at"`
	}

	out := make([]cannedRow, 0, 16)
	for rows.Next() {
		var c cannedRow
		var raw []byte
		if err := rows.Scan(&c.ID, &c.TemplateName, &c.Persona, &c.Variant,
			&raw, &c.Priority, &c.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed: "+err.Error())
			return
		}
		c.RawJSON = raw
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "iterate failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"template": template,
		"persona":  persona,
		"rows":     out,
	})
}
