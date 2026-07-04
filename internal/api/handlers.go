package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/jolyonbrown/point.vote/internal/room"
)

// decodeJSON reads a ≤16KB JSON body into dst. An empty body decodes as the
// zero value, so endpoints whose fields are all optional accept bare POSTs.
// Content-Type is deliberately ignored: the curl quickstart uses -d without
// a header.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	body := http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(body)
	err := dec.Decode(dst)
	switch {
	case err == nil:
		// One JSON value per body: trailing data is rejected rather than
		// silently ignored, so the body limit can't be dodged by a small
		// valid prefix.
		if dec.More() {
			return room.ValidationError("unexpected data after JSON body")
		}
		return nil
	case errors.Is(err, io.EOF):
		return nil // empty body
	default:
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return errTooLarge
		}
		return room.ValidationError("invalid JSON: " + err.Error())
	}
}

func bearerToken(r *http.Request) string {
	if token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		return strings.TrimSpace(token)
	}
	return ""
}

// baseURL reconstructs the public origin for the URLs returned on room
// creation. TLS terminates at the Cloudflare edge, so trust
// X-Forwarded-Proto for the scheme.
func baseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (s *Server) handleCreateRoom(w http.ResponseWriter, r *http.Request) {
	if !s.limiter.allow(clientIP(r)) {
		writeJSON(w, http.StatusTooManyRequests,
			errBody("rate_limited", "room creation limit reached for this IP; try again later"))
		return
	}
	var req struct {
		Deck       json.RawMessage `json:"deck"`
		Subject    string          `json:"subject"`
		Context    string          `json:"context"`
		AutoReveal *bool           `json:"auto_reveal"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	deck, err := deckFrom(req.Deck)
	if err != nil {
		writeError(w, err)
		return
	}
	autoReveal := req.AutoReveal == nil || *req.AutoReveal
	st, err := s.Svc.CreateRoom(deck, req.Subject, req.Context, autoReveal)
	if err != nil {
		writeError(w, err)
		return
	}
	base := baseURL(r)
	api := base + "/api/v1/rooms/" + st.RoomID
	writeJSON(w, http.StatusCreated, map[string]string{
		"room_id":    st.RoomID,
		"web_url":    base + "/r/" + st.RoomID,
		"api_url":    api,
		"events_url": api + "/events",
		"mcp_url":    base + "/mcp",
	})
}

// deckFrom parses the deck union type: absent → default preset, "name" →
// preset, {"custom": [...]} → custom deck (validated by the domain).
func deckFrom(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return room.ResolvePreset(room.DefaultPreset)
	}
	if raw[0] == '"' {
		var name string
		if err := json.Unmarshal(raw, &name); err != nil {
			return nil, room.ValidationError(`deck must be a preset name or {"custom": [...]}`)
		}
		return room.ResolvePreset(name)
	}
	var obj struct {
		Custom []string `json:"custom"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil || obj.Custom == nil {
		return nil, room.ValidationError(`deck must be a preset name or {"custom": [...]}`)
	}
	return obj.Custom, nil
}

func (s *Server) handleJoin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Kind string `json:"kind"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	pid, token, err := s.Svc.Join(r.PathValue("id"), req.Name, req.Kind)
	if err != nil {
		writeError(w, err)
		return
	}
	setParticipantKind(r, req.Kind)
	writeJSON(w, http.StatusCreated, map[string]string{"participant_id": pid, "token": token})
}

func (s *Server) handleGetRoom(w http.ResponseWriter, r *http.Request) {
	st, err := s.Svc.State(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Vary", "Accept")
	if wantsPlainText(r) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, renderText(st))
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleVote(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Value     string `json:"value"`
		Rationale string `json:"rationale"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	kind, err := s.Svc.CastVote(r.PathValue("id"), bearerToken(r), req.Value, req.Rationale)
	if err != nil {
		writeError(w, err)
		return
	}
	setParticipantKind(r, kind)
	writeJSON(w, http.StatusOK, map[string]bool{"accepted": true})
}

func (s *Server) handleReveal(w http.ResponseWriter, r *http.Request) {
	st, err := s.Svc.Reveal(r.PathValue("id"), bearerToken(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleStartRound(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Subject string `json:"subject"`
		Context string `json:"context"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	st, err := s.Svc.StartRound(r.PathValue("id"), bearerToken(r), req.Subject, req.Context)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, st)
}

func (s *Server) handleReact(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Emoji string `json:"emoji"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	if err := s.Svc.React(r.PathValue("id"), bearerToken(r), req.Emoji); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"accepted": true})
}

func (s *Server) handleLeave(w http.ResponseWriter, r *http.Request) {
	if err := s.Svc.Leave(r.PathValue("id"), bearerToken(r)); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
