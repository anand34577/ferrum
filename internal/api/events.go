package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// sseKeepAlive is how often a comment line ("\n:\n\n") is written to an
// idle SSE connection — long enough to not spam, short enough to stay under
// typical proxy/load-balancer idle-connection timeouts (60s is common).
const sseKeepAlive = 25 * time.Second

// streamEvents is GET /api/v1/events — a Server-Sent Events stream of
// internal/events.Bus activity (alert triggers/resolutions, connection
// health changes, and anything else published to the bus) for the
// authenticated session. Auth is the same cookie/API-key middleware as
// every other endpoint (see Router's requireAuth group); there is
// intentionally no per-event authorization beyond "is logged in" since the
// bus carries nothing more sensitive than what the REST endpoints already
// expose to any authenticated user.
func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	if s.events == nil {
		writeErrorMsg(w, http.StatusServiceUnavailable, "event stream is not available")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErrorMsg(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	ch, unsubscribe := s.events.Subscribe()
	defer unsubscribe()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no") // nginx: don't buffer the stream
	w.WriteHeader(http.StatusOK)

	// An initial comment flushes headers immediately so the client's
	// EventSource fires onopen right away instead of waiting for the first
	// real event (which may be minutes away).
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	ctx := r.Context()
	keepAlive := time.NewTicker(sseKeepAlive)
	defer keepAlive.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-keepAlive.C:
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case evt, ok := <-ch:
			if !ok {
				return
			}
			payload, err := json.Marshal(evt)
			if err != nil {
				slog.Error("event stream: marshaling event failed", "error", err)
				continue
			}
			// Deliberately left as the default "message" event (no "event:"
			// line) rather than one named per evt.Type: the type still
			// travels inside the JSON payload, so a generic
			// EventSource.onmessage listener can dispatch on it without the
			// caller having to know every type in advance or attach a
			// listener per type (see web/src/lib/useEventStream.ts).
			if _, err := fmt.Fprintf(w, "id: %s\ndata: %s\n\n", evt.ID, payload); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
