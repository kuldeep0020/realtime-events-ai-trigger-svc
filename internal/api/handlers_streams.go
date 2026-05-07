package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/sse"
)

// allowedStreams is the whitelist of stream names mounted on
// GET /api/streams/{stream}. Unknown streams return 404 to avoid the hub
// being misused as an arbitrary key/value channel by curious clients.
var allowedStreams = map[string]struct{}{
	sse.StreamEvents:     {},
	sse.StreamWindows:    {},
	sse.StreamTriggers:   {},
	sse.StreamMockEmails: {},
}

// handleSSEStream subscribes the client to the named SSE stream. Honours
// ctx cancellation (client disconnect) and ensures the subscription is
// unwound on return. Required headers per the SSE spec are set on first
// write so middlewares (compression, etc.) can't accidentally buffer.
func (s *Server) handleSSEStream(w http.ResponseWriter, r *http.Request) {
	stream := chi.URLParam(r, "stream")
	if _, ok := allowedStreams[stream]; !ok {
		writeError(w, http.StatusNotFound, "unknown stream: "+stream)
		return
	}

	// Server must support flushing — bail early with a clear error if not.
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "server does not support streaming")
		return
	}

	// Set SSE response headers BEFORE WriteHeader.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// X-Accel-Buffering: no — disables buffering at any nginx-style proxies.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// Send a "connected" comment immediately so EventSource's onopen fires.
	// Comments start with ':' per the SSE spec and are ignored by clients.
	if _, err := fmt.Fprintf(w, ": connected\n\n"); err != nil {
		return
	}
	flusher.Flush()

	ch, unsubscribe := s.hub.Subscribe(stream)
	defer unsubscribe()
	s.metrics.IncSSEConnect(stream)
	defer s.metrics.IncSSEDisconnect(stream)

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				// Hub dropped us (slow subscriber) or shut down.
				return
			}
			if err := writeSSEMessage(w, msg); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// writeSSEMessage writes a single message in the SSE wire format:
//
//	id: <id>\n
//	event: <event>\n
//	data: <json>\n
//	\n
//
// All fields are optional except `data`. The trailing blank line terminates
// the message — without it, the client buffers indefinitely.
func writeSSEMessage(w http.ResponseWriter, m sse.Message) error {
	if m.ID != "" {
		if _, err := fmt.Fprintf(w, "id: %s\n", m.ID); err != nil {
			return err
		}
	}
	if m.Event != "" {
		if _, err := fmt.Fprintf(w, "event: %s\n", m.Event); err != nil {
			return err
		}
	}
	body, err := json.Marshal(m.Data)
	if err != nil {
		// Replace failed payload with a small error envelope so the stream
		// keeps flowing.
		body = []byte(`{"error":"marshal_failed"}`)
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", body); err != nil {
		return err
	}
	return nil
}
