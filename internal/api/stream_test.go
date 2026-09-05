package api

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

type streamWriter struct {
	header    http.Header
	deadlines []time.Time
	writeErr  error
	flushErr  error
	flushed   bool
}

func (w *streamWriter) Header() http.Header { return w.header }
func (w *streamWriter) WriteHeader(int)     {}
func (w *streamWriter) Write(p []byte) (int, error) {
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	return len(p), nil
}
func (w *streamWriter) FlushError() error {
	w.flushed = true
	return w.flushErr
}
func (w *streamWriter) SetWriteDeadline(at time.Time) error {
	w.deadlines = append(w.deadlines, at)
	return nil
}

func TestSSEWriteDeadlineAndErrorsThroughMiddleware(t *testing.T) {
	broken := errors.New("connection failed")
	for _, stage := range []string{"success", "write", "flush"} {
		t.Run(stage, func(t *testing.T) {
			raw := &streamWriter{header: make(http.Header)}
			if stage == "write" {
				raw.writeErr = broken
			}
			if stage == "flush" {
				raw.flushErr = broken
			}
			wrapped := &statusRecorder{ResponseWriter: raw}
			err := sendSSE(wrapped, "state", 1, map[string]int{"revision": 1})
			if stage == "success" && err != nil {
				t.Fatal(err)
			}
			if stage != "success" && !errors.Is(err, broken) {
				t.Fatalf("lost %s error: %v", stage, err)
			}
			if len(raw.deadlines) != 2 || raw.deadlines[0].IsZero() ||
				!raw.deadlines[1].IsZero() {
				t.Fatal("write deadline was not set and cleared through middleware")
			}
			if stage == "success" && !raw.flushed {
				t.Fatal("event was not flushed")
			}
		})
	}
}
