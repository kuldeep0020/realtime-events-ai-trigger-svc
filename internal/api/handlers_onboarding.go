package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/db"
)

// generateConfigRequest is the body shape for POST /api/onboarding/generate-config.
// We deliberately accept arbitrary `answers` (map[string]any) so the wizard can
// evolve its question set without a backend round-trip; only `persona` is
// required.
type generateConfigRequest struct {
	Persona string         `json:"persona"`
	Answers map[string]any `json:"answers"`
}

// generateConfigResponse mirrors what the frontend expects: a YAML config
// string the user can preview and edit, plus the seed source path it came
// from for traceability.
type generateConfigResponse struct {
	Persona     string `json:"persona"`
	Source      string `json:"source"`       // "preset:<name>"
	ConfigYAML  string `json:"config_yaml"`  // the YAML body
	Description string `json:"description"`
}

// handleGenerateConfig returns a PRESET-driven config based on persona. No
// LLM call. Real-estate persona returns the realestate.yaml; rs-self returns
// rs-self.yaml. Unknown personas → 400.
func (s *Server) handleGenerateConfig(w http.ResponseWriter, r *http.Request) {
	var req generateConfigRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	persona := strings.TrimSpace(strings.ToLower(req.Persona))
	if persona == "" {
		writeError(w, http.StatusBadRequest, "persona is required")
		return
	}

	var (
		seedPath    string
		description string
	)
	switch persona {
	case "realestate":
		seedPath = "persona-configs/realestate.yaml"
		description = "Real-estate listing portal — fires Slack pings to assigned realtors when high-intent visitors abandon a session."
	case "rs-self", "rsself", "rs_self":
		seedPath = "persona-configs/rs-self.yaml"
		description = "RudderStack self-serve onboarding — fires support emails when users get stuck on errors."
		persona = "rs-self"
	default:
		writeError(w, http.StatusBadRequest, "unsupported persona: "+req.Persona)
		return
	}

	if s.seed == nil {
		writeError(w, http.StatusServiceUnavailable, "seed filesystem not configured")
		return
	}
	body, err := s.seed.ReadFile(seedPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read preset config: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, generateConfigResponse{
		Persona:     persona,
		Source:      "preset:" + seedPath,
		ConfigYAML:  string(body),
		Description: description,
	})
}

// activateConfigRequest accepts either id (existing row) OR persona+config_yaml
// (insert + activate). Mutually exclusive — exactly one path must be specified.
type activateConfigRequest struct {
	ID         string `json:"id,omitempty"`
	Persona    string `json:"persona,omitempty"`
	TenantID   string `json:"tenant_id,omitempty"`
	ConfigYAML string `json:"config_yaml,omitempty"`
}

type activateConfigResponse struct {
	ID      string `json:"id"`
	Active  bool   `json:"active"`
	Persona string `json:"persona"`
}

// handleActivateConfig promotes the seeded config for a persona to active=true,
// deactivating all other configs for that persona. No new rows are ever
// inserted — the operation is idempotent.
//
// Accepted request shapes:
//   - {"persona":"realestate"} — promotes the oldest config that has rules.
//   - {"persona":"realestate","config_yaml":"..."} — same; config_yaml is
//     ignored (the seeded config already has the correct YAML).
//   - {"id":"<uuid>"} — legacy path: promotes a specific config by UUID.
func (s *Server) handleActivateConfig(w http.ResponseWriter, r *http.Request) {
	if s.pool == nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	var req activateConfigRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	switch {
	case req.ID != "":
		// Legacy path: promote a specific config by UUID. Used when the caller
		// already knows the exact config row to activate (e.g. admin tooling).
		parsed, err := uuid.Parse(req.ID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid id (not a UUID)")
			return
		}

		var persona string
		if err := s.pool.QueryRow(ctx, `SELECT persona FROM configs WHERE id = $1`, parsed).Scan(&persona); err != nil {
			writeError(w, http.StatusNotFound, "config not found")
			return
		}

		tx, err := s.pool.Begin(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to begin tx: "+err.Error())
			return
		}
		defer func() { _ = tx.Rollback(ctx) }()

		if _, err := tx.Exec(ctx, `UPDATE configs SET active = FALSE WHERE persona = $1 AND id <> $2`, persona, parsed); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to deactivate prior configs: "+err.Error())
			return
		}
		if _, err := tx.Exec(ctx, `UPDATE configs SET active = TRUE WHERE id = $1`, parsed); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to activate config: "+err.Error())
			return
		}
		if err := tx.Commit(ctx); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to commit: "+err.Error())
			return
		}

		writeJSON(w, http.StatusOK, activateConfigResponse{
			ID:      parsed.String(),
			Active:  true,
			Persona: persona,
		})

	case req.Persona != "":
		// Wizard path: promote the seeded config (oldest config with rules) for
		// this persona. Never inserts a new row. config_yaml is accepted but
		// intentionally not applied — the demo uses preset configs and altering
		// the YAML live is out of scope.
		seededID, err := db.ActivatePersonaSeededConfig(ctx, s.pool, req.Persona)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "no config with rules found for persona: "+req.Persona)
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to activate config: "+err.Error())
			return
		}

		writeJSON(w, http.StatusOK, activateConfigResponse{
			ID:      seededID.String(),
			Active:  true,
			Persona: req.Persona,
		})

	default:
		writeError(w, http.StatusBadRequest, "either id or persona is required")
		return
	}
}

