package api

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestStaticRoutes(t *testing.T) {
	ts := testServer(t)

	tests := []struct {
		path        string
		contentType string
		contains    string
	}{
		{"/", "text/html; charset=utf-8", "Planning poker for humans"},
		{"/r/mint-otter-42", "text/html; charset=utf-8", "Pull up a chair"},
		{"/r/anything-goes-00", "text/html; charset=utf-8", "point"},
		{"/app.js", "text/javascript; charset=utf-8", "EventSource"},
		{"/style.css", "text/css; charset=utf-8", "--accent"},
		{"/llms.txt", "text/plain; charset=utf-8", "blindness is\nthe point"},
		{"/openapi.yaml", "application/yaml; charset=utf-8", "openapi: 3.1.0"},
		{"/skill", "text/markdown; charset=utf-8", "name: point-vote"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			resp, err := http.Get(ts.URL + tt.path)
			if err != nil {
				t.Fatalf("GET %s: %v", tt.path, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200", tt.path, resp.StatusCode)
			}
			if got := resp.Header.Get("Content-Type"); got != tt.contentType {
				t.Fatalf("Content-Type = %q, want %q", got, tt.contentType)
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			if !strings.Contains(string(body), tt.contains) {
				t.Fatalf("GET %s body missing %q", tt.path, tt.contains)
			}
		})
	}

	// The root pattern must be exact: unknown paths still 404.
	resp, err := http.Get(ts.URL + "/definitely-not-a-page")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /definitely-not-a-page = %d, want 404", resp.StatusCode)
	}
}

// TestUIUsesOnlyPublicAPI greps the client script for URL paths: every
// endpoint it calls must be under /api/v1 or a static asset — agents are
// users, so the browser gets no private surface (PLAN.md §1.2).
func TestUIUsesOnlyPublicAPI(t *testing.T) {
	ts := testServer(t)
	resp, err := http.Get(ts.URL + "/app.js")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	js, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(js), `"/api/v1/rooms"`) ||
		!strings.Contains(string(js), `"/api/v1/rooms/"`) {
		t.Fatal("app.js does not reference the public API base paths")
	}
	for _, private := range []string{"/internal", "/admin", "/api/v2"} {
		if strings.Contains(string(js), private) {
			t.Fatalf("app.js references a non-public path %q", private)
		}
	}
}
