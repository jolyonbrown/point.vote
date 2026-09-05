package api

import (
	"encoding/json"
	"errors"
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

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache, no-transform")
	w.WriteHeader(http.StatusOK)

	// Initial full snapshot. This also handles Last-Event-ID naively: a
	// reconnecting client just gets current state again, which is complete
	// by construction.
	st, err := s.Svc.State(id)
	if err != nil {
		return
	}
	if err := sendSSE(w, "state", st.Revision, st); err != nil {
		return
	}
	lastEventID := st.Revision

	hb := time.NewTicker(s.Heartbeat)
	defer hb.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-hb.C:
			// A named event, not an SSE comment: comments are invisible
			// to browser JavaScript, so a client can't tell a quiet
			// stream from a dead one. A visible ping lets the UI run a
			// liveness watchdog and rebuild zombie connections (TLS-
			// inspecting proxies buffer streams without erroring).
			if err := sendSSE(w, "ping", 0, struct{}{}); err != nil {
				return
			}
		case ev, open := <-ch:
			if !open {
				return // room expired
			}
			// The initial snapshot may already include events queued between
			// subscribing and reading state. Never send an older snapshot after it.
			if ev.ID <= lastEventID {
				continue
			}
			lastEventID = ev.ID
			if ev.Reaction != nil {
				err = sendSSE(w, ev.Name, ev.ID, ev.Reaction)
			} else {
				err = sendSSE(w, ev.Name, ev.ID, ev.State)
			}
			if err != nil {
				return
			}
		}
	}
}

// writeSSE emits one event. payload is the full room state for every event
// except "reaction", whose payload is the transient Reaction itself.
func writeSSE(w io.Writer, name string, id int, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if id > 0 {
		if _, err := fmt.Fprintf(w, "id: %d\n", id); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, data)
	return err
}

// Bound each write, not the lifetime of the stream. A disconnected or
// non-reading client must not retain a handler indefinitely.
func sendSSE(w http.ResponseWriter, name string, id int, payload any) error {
	controller := http.NewResponseController(w)
	if err := controller.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	defer controller.SetWriteDeadline(time.Time{}) // idle streams wait longer than the write deadline
	if err := writeSSE(w, name, id, payload); err != nil {
		return err
	}
	return controller.Flush()
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
