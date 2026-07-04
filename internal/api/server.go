// Package api implements the public HTTP surface: the REST API, SSE stream,
// and operational routes. Handlers stay thin; the rules live in
// internal/room. The browser UI consumes exactly these endpoints — there are
// no private ones.
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/jolyonbrown/point.vote/internal/room"
)

// Defaults, overridable per instance (mainly by tests).
const (
	defaultHeartbeat     = 25 * time.Second // keeps proxies from reaping idle SSE streams
	defaultCreatePerHour = 30               // per-IP room creation budget (PLAN.md §3)
	maxBodyBytes         = 16 << 10         // request bodies ≤ 16KB
	maxLongPollSeconds   = 55               // fits under Cloudflare's ~100s proxy timeout
)

// Server bundles the HTTP surface's dependencies and knobs. Zero values get
// sensible defaults in Handler.
type Server struct {
	Log           *slog.Logger
	Svc           *room.Service
	MCP           http.Handler // mounted at /mcp when non-nil
	Heartbeat     time.Duration
	CreatePerHour int

	limiter *ipLimiter
}

// Handler builds the root handler for the entire HTTP surface.
func (s *Server) Handler() http.Handler {
	if s.Heartbeat == 0 {
		s.Heartbeat = defaultHeartbeat
	}
	if s.CreatePerHour == 0 {
		s.CreatePerHour = defaultCreatePerHour
	}
	s.limiter = newIPLimiter(s.CreatePerHour)

	mux := http.NewServeMux()
	mountStatic(mux)
	if s.MCP != nil {
		mux.Handle("/mcp", s.MCP)
	}
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("POST /api/v1/rooms", s.handleCreateRoom)
	mux.HandleFunc("POST /api/v1/rooms/{id}/participants", s.handleJoin)
	mux.HandleFunc("GET /api/v1/rooms/{id}", s.handleGetRoom)
	mux.HandleFunc("POST /api/v1/rooms/{id}/vote", s.handleVote)
	mux.HandleFunc("POST /api/v1/rooms/{id}/reveal", s.handleReveal)
	mux.HandleFunc("POST /api/v1/rooms/{id}/react", s.handleReact)
	mux.HandleFunc("POST /api/v1/rooms/{id}/settle", s.handleSettle)
	mux.HandleFunc("POST /api/v1/rooms/{id}/rounds", s.handleStartRound)
	mux.HandleFunc("DELETE /api/v1/rooms/{id}/participants/self", s.handleLeave)
	mux.HandleFunc("GET /api/v1/rooms/{id}/result", s.handleResult)
	mux.HandleFunc("GET /api/v1/rooms/{id}/events", s.handleEvents)
	return requestLogger(s.Log, mux)
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "rooms": s.Svc.RoomCount()})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
