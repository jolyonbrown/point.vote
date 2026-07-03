package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRoutes(t *testing.T) {
	h := New(slog.New(slog.DiscardHandler))

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{"healthz ok", http.MethodGet, "/healthz", http.StatusOK},
		{"healthz wrong method", http.MethodPost, "/healthz", http.StatusMethodNotAllowed},
		{"unknown path", http.MethodGet, "/nope", http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("%s %s: got status %d, want %d", tt.method, tt.path, rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestHealthzBody(t *testing.T) {
	h := New(slog.New(slog.DiscardHandler))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	var body struct {
		OK    bool `json:"ok"`
		Rooms int  `json:"rooms"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if !body.OK {
		t.Errorf("ok = false, want true")
	}
	if body.Rooms != 0 {
		t.Errorf("rooms = %d, want 0", body.Rooms)
	}
}
