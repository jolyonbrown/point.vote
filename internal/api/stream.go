package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/jolyonbrown/point.vote/internal/room"
)

// handleEvents serves the SSE stream. Every event's data payload is the
// full redacted room state — snapshots beat diffs for correctness and make
// clients trivial (PLAN.md §4).
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ch, cancel, err := s.Svc.Subscribe(id)
	if err != nil {
		writeError(w, err)
		return
	}
	defer cancel()

	fl, ok := w.(http.Flusher)
	if !ok {
		writeError(w, fmt.Errorf("streaming unsupported"))
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	// Initial full snapshot. This also handles Last-Event-ID naively: a
	// reconnecting client just gets current state again, which is complete
	// by construction.
	st, err := s.Svc.State(id)
	if err != nil {
		return
	}
	writeSSE(w, "state", 0, st)
	fl.Flush()

	hb := time.NewTicker(s.Heartbeat)
	defer hb.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-hb.C:
			io.WriteString(w, ": heartbeat\n\n")
			fl.Flush()
		case ev, open := <-ch:
			if !open {
				return // room expired
			}
			if ev.Reaction != nil {
				writeSSE(w, ev.Name, ev.ID, ev.Reaction)
			} else {
				writeSSE(w, ev.Name, ev.ID, ev.State)
			}
			fl.Flush()
		}
	}
}

// writeSSE emits one event. payload is the full room state for every event
// except "reaction", whose payload is the transient Reaction itself.
func writeSSE(w io.Writer, name string, id int, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	if id > 0 {
		fmt.Fprintf(w, "id: %d\n", id)
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, data)
}

// handleResult is the long-poll: it blocks until the current round is
// revealed or the timeout elapses, then returns room state either way. It
// lets curl-only agents wait without parsing SSE. The waiting itself lives
// in room.Service, shared with the MCP wait_for_reveal tool.
func (s *Server) handleResult(w http.ResponseWriter, r *http.Request) {
	timeout := 30 * time.Second
	if q := r.URL.Query().Get("timeout"); q != "" {
		secs, err := strconv.Atoi(q)
		if err != nil || secs < 0 {
			writeError(w, room.ValidationError("timeout must be a non-negative integer (seconds)"))
			return
		}
		timeout = time.Duration(min(secs, maxLongPollSeconds)) * time.Second
	}

	st, err := s.Svc.WaitForReveal(r.Context(), r.PathValue("id"), timeout)
	if err != nil {
		if r.Context().Err() != nil {
			return // client went away; nothing to write
		}
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}
