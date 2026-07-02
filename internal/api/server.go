// Package api implements the public HTTP surface: the REST API, SSE stream,
// and operational routes. Handlers stay thin; the rules live in
// internal/room.
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// New returns the root handler for the entire HTTP surface.
func New(logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	return requestLogger(logger, mux)
}

// handleHealthz reports liveness. The room count stays 0 until the store
// lands in phase 1.
func handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "rooms": 0})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
