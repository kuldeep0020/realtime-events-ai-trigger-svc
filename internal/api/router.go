// Package api implements the chi-based HTTP API and SSE streaming endpoints
// for the realtime-ai-trigger service (§3.10 of the design).
//
// Construction is dependency-injected so tests can swap in stub Postgres
// pools and an isolated SSE hub. The Server type bundles the chi.Router with
// shared dependencies and a Handler() entry point for httptest and main.
package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/sse"
)

// SeedFS abstracts read access to the seed/ directory. Production code uses
// a real os-rooted FS (looking for /etc/seed/ then ./seed/); tests can pass
// an in-memory implementation. It deliberately does NOT use io/fs.FS to
// avoid pulling persona-config plumbing into too many packages — a single
// ReadFile method is enough for the wizard endpoint.
type SeedFS interface {
	// ReadFile returns the bytes for the given path relative to the seed
	// root (e.g. "persona-configs/realestate.yaml"). Returns os.ErrNotExist
	// when missing.
	ReadFile(path string) ([]byte, error)
}

// FireScriptFunc is the optional callback invoked by POST /api/demo/fire-script.
// Stage 1 ships a 501 stub; stage 3 wiring sets this to a real demo-fire
// invocation. The function returns the count of events sent.
type FireScriptFunc func(ctx context.Context, persona string) (int, error)

// AdminSeedFunc is the optional callback invoked by POST /api/admin/seed.
// Stage 1 ships a 501 stub; the wired version calls seed.Seeder.LoadAll.
type AdminSeedFunc func(ctx context.Context) error

// Server wires the API. Construct via New, then mount via Handler().
type Server struct {
	pool   *pgxpool.Pool
	hub    *sse.Hub
	seed   SeedFS
	router *chi.Mux
	// metrics is the lightweight counter store used by /metrics. We keep
	// the Prometheus client lib out of the hackathon binary.
	metrics *metrics

	// fireScript is invoked by handleFireScript when set. Nil → 501 stub.
	fireScript FireScriptFunc

	// adminSeed is invoked by handleAdminSeed when set. Nil → 501 stub.
	adminSeed AdminSeedFunc

	// startedAt records process boot time for /metrics and /healthz.
	startedAt time.Time
}

// Config wires runtime options.
type Config struct {
	Pool       *pgxpool.Pool
	Hub        *sse.Hub
	Seed       SeedFS
	FireScript FireScriptFunc
	AdminSeed  AdminSeedFunc
}

// New builds a Server with the supplied dependencies and wires the chi
// router. Pool may be nil for tests that don't exercise DB-backed
// endpoints; in that case those endpoints return 503.
func New(cfg Config) *Server {
	s := &Server{
		pool:       cfg.Pool,
		hub:        cfg.Hub,
		seed:       cfg.Seed,
		fireScript: cfg.FireScript,
		adminSeed:  cfg.AdminSeed,
		metrics:    newMetrics(),
		startedAt:  time.Now().UTC(),
	}
	if s.hub == nil {
		s.hub = sse.NewHub()
	}
	s.router = s.buildRouter()
	return s
}

// Handler returns the http.Handler used by the main binary or by httptest.
func (s *Server) Handler() http.Handler { return s.router }

// Hub exposes the underlying SSE hub for the consumer/dispatcher path
// (so they can publish without re-constructing it).
func (s *Server) Hub() *sse.Hub { return s.hub }

// buildRouter mounts every endpoint per §3.10.
func (s *Server) buildRouter() *chi.Mux {
	r := chi.NewRouter()

	// Stable middleware. RequestID + Recoverer are universally useful;
	// Timeout is moderate so SSE handlers (which never return until the
	// client disconnects) bypass it via their own context handling.
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(s.metricsMiddleware)

	// Health / readiness / metrics — no /api prefix.
	r.Get("/healthz", s.handleHealthz)
	r.Get("/readyz", s.handleReadyz)
	r.Get("/metrics", s.handleMetrics)

	r.Route("/api", func(r chi.Router) {
		// Tracking plan (read from `tracking_plans` JSONB).
		r.Get("/tracking-plan/{persona}", s.handleGetTrackingPlan)

		// Onboarding wizard.
		r.Post("/onboarding/generate-config", s.handleGenerateConfig)
		r.Post("/onboarding/activate", s.handleActivateConfig)

		// SSE streams.
		r.Get("/streams/{stream}", s.handleSSEStream)

		// Demo controller.
		r.Post("/demo/fire-script", s.handleFireScript)
		r.Post("/demo/reset", s.handleDemoReset)
		r.Post("/demo/replay-last-trigger", s.handleReplayLastTrigger)

		// Mock email viewer.
		r.Get("/mock-emails", s.handleListMockEmails)

		// Admin.
		r.Post("/admin/seed", s.handleAdminSeed)
		r.Get("/admin/canned", s.handleAdminCanned)
	})

	return r
}
