package api

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// getText fetches the room with Accept: text/plain.
func getText(t *testing.T, url string) (string, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Accept", "text/plain")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return string(body), resp.Header.Get("Content-Type")
}

func TestWantsPlainText(t *testing.T) {
	tests := []struct {
		accept string
		want   bool
	}{
		{"text/plain", true},
		{"application/json, text/plain", true},
		{"text/plain; q=0.5", true},
		{"application/json, text/plain;q=0", false}, // explicit rejection
		{"text/plainish", false},                    // not a media-type match
		{"application/json; note=text/plain", false},
		{"text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8", false}, // browser default
		{"", false},
	}
	for _, tt := range tests {
		r, _ := http.NewRequest("GET", "/", nil)
		if tt.accept != "" {
			r.Header.Set("Accept", tt.accept)
		}
		if got := wantsPlainText(r); got != tt.want {
			t.Errorf("wantsPlainText(%q) = %v, want %v", tt.accept, got, tt.want)
		}
	}
}

func TestPlainTextRoom(t *testing.T) {
	const canary = "TEXT-PATH-CANARY"
	ts := testServer(t)
	ip := "10.20.0.1"
	roomID := createRoom(t, ts, ip, map[string]any{"subject": "Pick a colour"})
	_, alice := joinRoom(t, ts, ip, roomID, "Alice", "human")
	_, bob := joinRoom(t, ts, ip, roomID, "Bob", "human")
	joinRoom(t, ts, ip, roomID, "watcher", "observer")
	vote(t, ts, ip, roomID, alice, "5", canary)

	url := ts.URL + "/api/v1/rooms/" + roomID

	t.Run("voting: aligned, redacted", func(t *testing.T) {
		body, ct := getText(t, url)
		if ct != "text/plain; charset=utf-8" {
			t.Fatalf("Content-Type = %q", ct)
		}
		for _, want := range []string{"round 1 · voting", "Pick a colour", "deck: 0 1 2 3 5 8 13 21", "Alice", "voted", "watcher", "(observer)", "waiting on 1 of 2."} {
			if !strings.Contains(body, want) {
				t.Fatalf("text missing %q:\n%s", want, body)
			}
		}
		if strings.Contains(body, canary) {
			t.Fatalf("text rendering leaked rationale while voting:\n%s", body)
		}
		if strings.Contains(body, "results") {
			t.Fatalf("results section present while voting:\n%s", body)
		}
	})

	t.Run("json remains the default", func(t *testing.T) {
		resp, err := http.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
			t.Fatalf("default Content-Type = %q, want application/json", ct)
		}
	})

	t.Run("revealed: votes and stats", func(t *testing.T) {
		vote(t, ts, ip, roomID, bob, "13", "") // completes the round
		body, _ := getText(t, url)
		for _, want := range []string{"round 1 · revealed", "results", `"` + canary + `"`, "top 5/13 (tie) · spread 8 · median 9 · mean 9 · consensus false"} {
			if !strings.Contains(body, want) {
				t.Fatalf("revealed text missing %q:\n%s", want, body)
			}
		}
	})

	t.Run("settled: verdict and awards", func(t *testing.T) {
		status, _, _ := doJSON(t, "POST", ts.URL+"/api/v1/rooms/"+roomID+"/settle", alice, ip,
			map[string]string{"value": "8"})
		if status != http.StatusOK {
			t.Fatalf("settle = %d", status)
		}
		body, _ := getText(t, url)
		for _, want := range []string{"settled on 8 — called by Alice", "The Oracle:"} {
			if !strings.Contains(body, want) {
				t.Fatalf("settled text missing %q:\n%s", want, body)
			}
		}
	})

	t.Run("archived call shows in history", func(t *testing.T) {
		if status, _, _ := doJSON(t, "POST", ts.URL+"/api/v1/rooms/"+roomID+"/rounds", alice, ip, nil); status != http.StatusCreated {
			t.Fatalf("new round = %d", status)
		}
		body, _ := getText(t, url)
		if strings.Contains(body, "settled on 8") {
			t.Fatalf("live verdict should clear on new round:\n%s", body)
		}
		if !strings.Contains(body, "· called 8") {
			t.Fatalf("history line missing archived call:\n%s", body)
		}
	})
}
