package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// chiRouteContext is a tiny indirection so other files in the package can
// reach into chi without importing it directly.
func chiRouteContext(r *http.Request) *chi.Context {
	return chi.RouteContext(r.Context())
}

// handleHealthz always returns 200 — the process is alive if it can serve.
func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"started": s.startedAt.Format(time.RFC3339),
	})
}

// handleReadyz returns 200 only if the Postgres pool can be pinged. When
// the pool is nil (test mode), readyz still returns 200 — readiness is
// scoped to "has the binary fully booted" not "is every dependency up".
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if s.pool == nil {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ready", "db": "skipped"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.pool.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "unready",
			"error":  err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ready", "db": "ok"})
}

// handleMetrics emits the in-process counters in Prometheus text format.
func (s *Server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(s.metrics.Render()))
}
