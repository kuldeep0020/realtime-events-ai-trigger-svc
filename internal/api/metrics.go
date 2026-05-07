package api

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
)

// metrics is a tiny in-process counter store used by /metrics. The hackathon
// brief explicitly says "no prom client lib" — we emit Prometheus text
// format manually because that's the lowest-friction shape consumers can
// scrape if they want to.
type metrics struct {
	mu sync.RWMutex
	// reqs[method+path] -> count. We don't bound cardinality here because
	// only chi-routed paths are recorded (template paths, not actual params).
	reqs map[string]*atomic.Uint64

	// SSE-specific counters.
	sseConnectsByStream    map[string]*atomic.Uint64
	sseDisconnectsByStream map[string]*atomic.Uint64
}

func newMetrics() *metrics {
	return &metrics{
		reqs:                   make(map[string]*atomic.Uint64),
		sseConnectsByStream:    make(map[string]*atomic.Uint64),
		sseDisconnectsByStream: make(map[string]*atomic.Uint64),
	}
}

// IncRequest increments the per-route counter. The label uses chi's route
// pattern (not the actual concrete URL) so cardinality stays bounded.
func (m *metrics) IncRequest(method, routePattern string) {
	if routePattern == "" {
		routePattern = "unknown"
	}
	key := method + " " + routePattern
	c := m.getOrCreate(&m.reqs, key)
	c.Add(1)
}

// IncSSEConnect / IncSSEDisconnect track per-stream subscribe/unsubscribe.
func (m *metrics) IncSSEConnect(stream string) {
	c := m.getOrCreate(&m.sseConnectsByStream, stream)
	c.Add(1)
}
func (m *metrics) IncSSEDisconnect(stream string) {
	c := m.getOrCreate(&m.sseDisconnectsByStream, stream)
	c.Add(1)
}

func (m *metrics) getOrCreate(m1 *map[string]*atomic.Uint64, key string) *atomic.Uint64 {
	m.mu.RLock()
	c, ok := (*m1)[key]
	m.mu.RUnlock()
	if ok {
		return c
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok = (*m1)[key]; ok {
		return c
	}
	c = &atomic.Uint64{}
	(*m1)[key] = c
	return c
}

// Render produces a text/plain Prometheus-compatible body. Counters only.
func (m *metrics) Render() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var b strings.Builder
	b.WriteString("# TYPE http_requests_total counter\n")
	for k, v := range m.reqs {
		// k is "<METHOD> <ROUTE>" — encode safely as labels.
		method, route, _ := strings.Cut(k, " ")
		fmt.Fprintf(&b, "http_requests_total{method=%q,route=%q} %d\n", method, route, v.Load())
	}
	b.WriteString("# TYPE sse_connects_total counter\n")
	for stream, v := range m.sseConnectsByStream {
		fmt.Fprintf(&b, "sse_connects_total{stream=%q} %d\n", stream, v.Load())
	}
	b.WriteString("# TYPE sse_disconnects_total counter\n")
	for stream, v := range m.sseDisconnectsByStream {
		fmt.Fprintf(&b, "sse_disconnects_total{stream=%q} %d\n", stream, v.Load())
	}
	return b.String()
}

// metricsMiddleware records one increment per request. It runs after chi
// has determined the route pattern, so labels are bounded.
func (s *Server) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		// chi.RouteContext is populated by the time the handler returns.
		ctx := chiRouteContext(r)
		pattern := ""
		if ctx != nil {
			pattern = ctx.RoutePattern()
		}
		s.metrics.IncRequest(r.Method, pattern)
	})
}
