package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

// handleGetTrackingPlan returns the JSONB `spec` from `tracking_plans` for
// the given persona. 404 when missing; 503 when DB is unavailable.
func (s *Server) handleGetTrackingPlan(w http.ResponseWriter, r *http.Request) {
	persona := chi.URLParam(r, "persona")
	if persona == "" {
		writeError(w, http.StatusBadRequest, "persona is required")
		return
	}
	if s.pool == nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	var spec []byte
	err := s.pool.QueryRow(ctx,
		`SELECT spec FROM tracking_plans WHERE persona = $1 LIMIT 1`, persona,
	).Scan(&spec)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "tracking plan not found for persona")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load tracking plan")
		return
	}

	// spec is JSONB; pass through as-is. We use json.RawMessage to avoid a
	// double encode/decode roundtrip.
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"persona": persona,
		"spec":    json.RawMessage(spec),
	})
}
