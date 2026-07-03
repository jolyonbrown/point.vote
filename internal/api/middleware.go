package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// logExtra carries request-scoped log fields that only the handler can
// know, back to the request logger (PLAN.md §7: participant_kind "where
// known"). Same-goroutine access only; no locking.
type logExtra struct {
	participantKind string
}

type logExtraKey struct{}

// setParticipantKind annotates the request log line for r, when the
// handler learns who is calling.
func setParticipantKind(r *http.Request, kind string) {
	if ex, ok := r.Context().Value(logExtraKey{}).(*logExtra); ok {
		ex.participantKind = kind
	}
}

// requestLogger emits one structured log line per request (PLAN.md §7).
// Never log vote values here — while voting they must not appear anywhere,
// logs included.
func requestLogger(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ex := &logExtra{}
		r = r.WithContext(context.WithValue(r.Context(), logExtraKey{}, ex))
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
		}
		if id := roomIDFromPath(r.URL.Path); id != "" {
			attrs = append(attrs, "room_id", id)
		}
		if ex.participantKind != "" {
			attrs = append(attrs, "participant_kind", ex.participantKind)
		}
		logger.Info("request", attrs...)
	})
}

func roomIDFromPath(path string) string {
	rest, ok := strings.CutPrefix(path, "/api/v1/rooms/")
	if !ok {
		return ""
	}
	id, _, _ := strings.Cut(rest, "/")
	return id
}

// statusRecorder captures the status code for logging. It forwards Flush so
// streaming responses (SSE) keep working behind the middleware.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
