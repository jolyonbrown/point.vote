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
			writeSSE(w, ev.Name, ev.ID, ev.State)
			fl.Flush()
		}
	}
}

func writeSSE(w io.Writer, name string, id int, st room.State) {
	data, err := json.Marshal(st)
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
// lets curl-only agents wait without parsing SSE.
func (s *Server) handleResult(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	timeout := 30 * time.Second
	if q := r.URL.Query().Get("timeout"); q != "" {
		secs, err := strconv.Atoi(q)
		if err != nil || secs < 0 {
			writeError(w, room.ValidationError("timeout must be a non-negative integer (seconds)"))
			return
		}
		timeout = time.Duration(min(secs, maxLongPollSeconds)) * time.Second
	}

	// Subscribe before the first state check so a reveal can't slip between
	// the two.
	ch, cancel, err := s.Svc.Subscribe(id)
	if err != nil {
		writeError(w, err)
		return
	}
	defer cancel()

	st, err := s.Svc.State(id)
	if err != nil {
		writeError(w, err)
		return
	}
	if st.Round.State == room.StateRevealed || timeout == 0 {
		writeJSON(w, http.StatusOK, st)
		return
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	// Subscriber channels drop events rather than block (slow-consumer
	// rule), so a reveal could in principle be missed under a stampede;
	// the recheck ticker bounds that wait to two seconds.
	recheck := time.NewTicker(2 * time.Second)
	defer recheck.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-timer.C:
			st, err := s.Svc.State(id)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, st)
			return
		case <-recheck.C:
			st, err := s.Svc.State(id)
			if err != nil {
				writeError(w, err)
				return
			}
			if st.Round.State == room.StateRevealed {
				writeJSON(w, http.StatusOK, st)
				return
			}
		case ev, open := <-ch:
			if !open {
				writeError(w, room.ErrRoomNotFound)
				return
			}
			if ev.State.Round.State == room.StateRevealed {
				writeJSON(w, http.StatusOK, ev.State)
				return
			}
		}
	}
}
