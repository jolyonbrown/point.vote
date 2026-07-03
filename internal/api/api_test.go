package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jolyonbrown/point.vote/internal/room"
)

func testServer(t *testing.T) *httptest.Server {
	t.Helper()
	svc := room.NewService(room.NewMemStore(), slog.New(slog.DiscardHandler))
	srv := &Server{
		Log:       slog.New(slog.DiscardHandler),
		Svc:       svc,
		Heartbeat: 50 * time.Millisecond,
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

// doJSON sends a request and decodes the response body. Each test passes a
// distinct fake client IP so the create rate limiter can't couple tests.
func doJSON(t *testing.T, method, url, token, ip string, body any) (int, map[string]any, string) {
	t.Helper()
	var buf io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		buf = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, buf)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("X-Forwarded-For", ip)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var decoded map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("decode %q: %v", raw, err)
		}
	}
	return resp.StatusCode, decoded, string(raw)
}

func createRoom(t *testing.T, ts *httptest.Server, ip string, body any) string {
	t.Helper()
	status, resp, _ := doJSON(t, "POST", ts.URL+"/api/v1/rooms", "", ip, body)
	if status != http.StatusCreated {
		t.Fatalf("create room status = %d, resp %v", status, resp)
	}
	id, _ := resp["room_id"].(string)
	if id == "" {
		t.Fatalf("create room returned no room_id: %v", resp)
	}
	return id
}

func joinRoom(t *testing.T, ts *httptest.Server, ip, roomID, name, kind string) (pid, token string) {
	t.Helper()
	status, resp, _ := doJSON(t, "POST", ts.URL+"/api/v1/rooms/"+roomID+"/participants", "", ip,
		map[string]string{"name": name, "kind": kind})
	if status != http.StatusCreated {
		t.Fatalf("join status = %d, resp %v", status, resp)
	}
	return resp["participant_id"].(string), resp["token"].(string)
}

func vote(t *testing.T, ts *httptest.Server, ip, roomID, token, value, rationale string) {
	t.Helper()
	status, resp, _ := doJSON(t, "POST", ts.URL+"/api/v1/rooms/"+roomID+"/vote", token, ip,
		map[string]string{"value": value, "rationale": rationale})
	if status != http.StatusOK {
		t.Fatalf("vote status = %d, resp %v", status, resp)
	}
}

func TestHealthz(t *testing.T) {
	ts := testServer(t)
	status, resp, _ := doJSON(t, "GET", ts.URL+"/healthz", "", "10.0.0.1", nil)
	if status != http.StatusOK || resp["ok"] != true {
		t.Fatalf("healthz = %d %v", status, resp)
	}
	if resp["rooms"].(float64) != 0 {
		t.Fatalf("rooms = %v, want 0", resp["rooms"])
	}
	createRoom(t, ts, "10.0.0.1", nil)
	_, resp, _ = doJSON(t, "GET", ts.URL+"/healthz", "", "10.0.0.1", nil)
	if resp["rooms"].(float64) != 1 {
		t.Fatalf("rooms = %v after create, want 1", resp["rooms"])
	}
}

// TestFullFlowRedaction runs the README flow and proves the redaction
// property on the plain GET read path.
func TestFullFlowRedaction(t *testing.T) {
	const canary = "GET-PATH-RATIONALE-CANARY"
	ts := testServer(t)
	ip := "10.1.0.1"

	roomID := createRoom(t, ts, ip, map[string]any{
		"deck": "fibonacci", "subject": "Add OAuth token refresh",
	})
	_, alice := joinRoom(t, ts, ip, roomID, "Alice", "human")
	_, claude := joinRoom(t, ts, ip, roomID, "claude-code", "agent")

	vote(t, ts, ip, roomID, alice, "5", canary)

	status, resp, raw := doJSON(t, "GET", ts.URL+"/api/v1/rooms/"+roomID, "", ip, nil)
	if status != http.StatusOK {
		t.Fatalf("GET room = %d", status)
	}
	if resp["results"] != nil {
		t.Fatalf("results = %v while voting, want null", resp["results"])
	}
	if strings.Contains(raw, canary) {
		t.Fatalf("GET leaked rationale while voting: %s", raw)
	}
	round := resp["round"].(map[string]any)
	if round["state"] != "voting" || round["votes_cast"].(float64) != 1 {
		t.Fatalf("round = %v", round)
	}
	for _, p := range round["participants"].([]any) {
		pm := p.(map[string]any)
		wantVoted := pm["name"] == "Alice"
		if pm["has_voted"] != wantVoted {
			t.Fatalf("participant %v has_voted = %v, want %v", pm["name"], pm["has_voted"], wantVoted)
		}
	}

	vote(t, ts, ip, roomID, claude, "13", "token rotation bites")

	_, resp, raw = doJSON(t, "GET", ts.URL+"/api/v1/rooms/"+roomID, "", ip, nil)
	results, ok := resp["results"].(map[string]any)
	if !ok {
		t.Fatalf("no results after every voter voted (auto-reveal): %s", raw)
	}
	if !strings.Contains(raw, canary) {
		t.Fatal("revealed state missing rationale")
	}
	stats := results["stats"].(map[string]any)
	for field, want := range map[string]float64{"min": 5, "max": 13, "spread": 8, "median": 9, "mean": 9} {
		if got := stats[field].(float64); got != want {
			t.Fatalf("stats.%s = %v, want %v", field, got, want)
		}
	}
	if stats["consensus"] != false {
		t.Fatalf("consensus = %v, want false", stats["consensus"])
	}
	counts := stats["counts"].(map[string]any)
	if counts["5"].(float64) != 1 || counts["13"].(float64) != 1 {
		t.Fatalf("counts = %v", counts)
	}

	// Next round archives history.
	status, resp, _ = doJSON(t, "POST", ts.URL+"/api/v1/rooms/"+roomID+"/rounds", alice, ip,
		map[string]string{"subject": "round two"})
	if status != http.StatusCreated {
		t.Fatalf("start round = %d %v", status, resp)
	}
	if n := len(resp["history"].([]any)); n != 1 {
		t.Fatalf("history len = %d, want 1", n)
	}
	if resp["round"].(map[string]any)["seq"].(float64) != 2 {
		t.Fatal("seq did not increment")
	}
}

func TestErrorCases(t *testing.T) {
	ts := testServer(t)
	ip := "10.2.0.1"
	roomID := createRoom(t, ts, ip, nil)
	_, token := joinRoom(t, ts, ip, roomID, "Alice", "human")
	_, obsToken := joinRoom(t, ts, ip, roomID, "watcher", "observer")

	tests := []struct {
		name       string
		method     string
		path       string
		token      string
		body       any
		wantStatus int
		wantCode   string
	}{
		{"unknown room", "GET", "/api/v1/rooms/no-such-room-00", "", nil, 404, "not_found"},
		{"bad kind", "POST", "/api/v1/rooms/" + roomID + "/participants", "", map[string]string{"name": "x", "kind": "robot"}, 400, "validation"},
		{"empty name", "POST", "/api/v1/rooms/" + roomID + "/participants", "", map[string]string{"name": "", "kind": "human"}, 400, "validation"},
		{"vote no token", "POST", "/api/v1/rooms/" + roomID + "/vote", "", map[string]string{"value": "5"}, 401, "bad_token"},
		{"vote bad token", "POST", "/api/v1/rooms/" + roomID + "/vote", "wrong", map[string]string{"value": "5"}, 401, "bad_token"},
		{"vote off deck", "POST", "/api/v1/rooms/" + roomID + "/vote", token, map[string]string{"value": "4"}, 400, "validation"},
		{"observer votes", "POST", "/api/v1/rooms/" + roomID + "/vote", obsToken, map[string]string{"value": "5"}, 403, "forbidden"},
		{"round while voting", "POST", "/api/v1/rooms/" + roomID + "/rounds", token, nil, 409, "wrong_state"},
		{"unknown preset", "POST", "/api/v1/rooms", "", map[string]any{"deck": "tarot"}, 400, "validation"},
		{"deck too small", "POST", "/api/v1/rooms", "", map[string]any{"deck": map[string]any{"custom": []string{"solo"}}}, 400, "validation"},
		{"malformed json", "POST", "/api/v1/rooms", "", nil, 400, "validation"}, // body sent below
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var status int
			var resp map[string]any
			if tt.name == "malformed json" {
				req, _ := http.NewRequest("POST", ts.URL+tt.path, strings.NewReader("{not json"))
				req.Header.Set("X-Forwarded-For", ip)
				res, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer res.Body.Close()
				status = res.StatusCode
				_ = json.NewDecoder(res.Body).Decode(&resp)
			} else {
				status, resp, _ = doJSON(t, tt.method, ts.URL+tt.path, tt.token, ip, tt.body)
			}
			if status != tt.wantStatus {
				t.Fatalf("status = %d, want %d (resp %v)", status, tt.wantStatus, resp)
			}
			errObj, _ := resp["error"].(map[string]any)
			if errObj == nil || errObj["code"] != tt.wantCode {
				t.Fatalf("error = %v, want code %q", resp["error"], tt.wantCode)
			}
		})
	}

	t.Run("vote then reveal then conflict", func(t *testing.T) {
		// auto_reveal off so the reveal is manual.
		rid := createRoom(t, ts, ip, map[string]any{"auto_reveal": false})
		_, tok := joinRoom(t, ts, ip, rid, "Bob", "human")
		vote(t, ts, ip, rid, tok, "5", "")
		if status, _, _ := doJSON(t, "POST", ts.URL+"/api/v1/rooms/"+rid+"/reveal", tok, ip, nil); status != 200 {
			t.Fatalf("reveal = %d", status)
		}
		if status, resp, _ := doJSON(t, "POST", ts.URL+"/api/v1/rooms/"+rid+"/reveal", tok, ip, nil); status != 409 {
			t.Fatalf("double reveal = %d %v, want 409", status, resp)
		}
		if status, resp, _ := doJSON(t, "POST", ts.URL+"/api/v1/rooms/"+rid+"/vote", tok, ip, map[string]string{"value": "8"}); status != 409 {
			t.Fatalf("vote after reveal = %d %v, want 409", status, resp)
		}
	})
}

func TestLeave(t *testing.T) {
	ts := testServer(t)
	ip := "10.3.0.1"
	roomID := createRoom(t, ts, ip, nil)
	_, token := joinRoom(t, ts, ip, roomID, "Alice", "human")

	status, _, _ := doJSON(t, "DELETE", ts.URL+"/api/v1/rooms/"+roomID+"/participants/self", token, ip, nil)
	if status != http.StatusNoContent {
		t.Fatalf("leave = %d, want 204", status)
	}
	// Token dies with the participant.
	status, _, _ = doJSON(t, "POST", ts.URL+"/api/v1/rooms/"+roomID+"/vote", token, ip, map[string]string{"value": "5"})
	if status != http.StatusUnauthorized {
		t.Fatalf("vote after leave = %d, want 401", status)
	}
	_, resp, _ := doJSON(t, "GET", ts.URL+"/api/v1/rooms/"+roomID, "", ip, nil)
	if n := len(resp["round"].(map[string]any)["participants"].([]any)); n != 0 {
		t.Fatalf("%d participants after leave, want 0", n)
	}
}

func TestCreateRoomResponse(t *testing.T) {
	ts := testServer(t)
	status, resp, _ := doJSON(t, "POST", ts.URL+"/api/v1/rooms", "", "10.4.0.1",
		map[string]any{"deck": "tshirt"})
	if status != http.StatusCreated {
		t.Fatalf("create = %d", status)
	}
	id := resp["room_id"].(string)
	for field, suffix := range map[string]string{
		"web_url":    "/r/" + id,
		"api_url":    "/api/v1/rooms/" + id,
		"events_url": "/api/v1/rooms/" + id + "/events",
		"mcp_url":    "/mcp",
	} {
		got, _ := resp[field].(string)
		if !strings.HasPrefix(got, ts.URL) || !strings.HasSuffix(got, suffix) {
			t.Fatalf("%s = %q, want %s…%s", field, got, ts.URL, suffix)
		}
	}
}

func TestCustomDeck(t *testing.T) {
	ts := testServer(t)
	ip := "10.5.0.1"
	roomID := createRoom(t, ts, ip, map[string]any{
		"deck": map[string]any{"custom": []string{"postgres", "sqlite", "dynamo"}},
	})
	_, token := joinRoom(t, ts, ip, roomID, "Alice", "human")
	vote(t, ts, ip, roomID, token, "sqlite", "boring wins")

	_, resp, _ := doJSON(t, "GET", ts.URL+"/api/v1/rooms/"+roomID, "", ip, nil)
	results := resp["results"].(map[string]any) // sole voter → auto-revealed
	stats := results["stats"].(map[string]any)
	if stats["min"] != nil || stats["consensus"] != false {
		t.Fatalf("non-numeric deck stats = %v", stats)
	}
	if stats["counts"].(map[string]any)["sqlite"].(float64) != 1 {
		t.Fatalf("counts = %v", stats["counts"])
	}
}

func TestLongPoll(t *testing.T) {
	ts := testServer(t)
	ip := "10.6.0.1"
	roomID := createRoom(t, ts, ip, nil)
	_, alice := joinRoom(t, ts, ip, roomID, "Alice", "human")
	_, bob := joinRoom(t, ts, ip, roomID, "Bob", "human")

	t.Run("timeout returns voting state, redacted", func(t *testing.T) {
		const canary = "LONGPOLL-CANARY"
		vote(t, ts, ip, roomID, alice, "5", canary)
		start := time.Now()
		status, resp, raw := doJSON(t, "GET", ts.URL+"/api/v1/rooms/"+roomID+"/result?timeout=1", "", ip, nil)
		if status != http.StatusOK {
			t.Fatalf("long-poll = %d", status)
		}
		if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
			t.Fatalf("long-poll returned in %v, should have blocked ~1s", elapsed)
		}
		if resp["results"] != nil || strings.Contains(raw, canary) {
			t.Fatalf("long-poll leaked votes while voting: %s", raw)
		}
	})

	t.Run("returns on reveal", func(t *testing.T) {
		go func() {
			time.Sleep(150 * time.Millisecond)
			vote(t, ts, ip, roomID, bob, "8", "") // completes the round → auto-reveal
		}()
		start := time.Now()
		status, resp, _ := doJSON(t, "GET", ts.URL+"/api/v1/rooms/"+roomID+"/result?timeout=10", "", ip, nil)
		if status != http.StatusOK {
			t.Fatalf("long-poll = %d", status)
		}
		if time.Since(start) > 5*time.Second {
			t.Fatal("long-poll did not return promptly on reveal")
		}
		if resp["results"] == nil {
			t.Fatal("long-poll returned without results after reveal")
		}
	})

	t.Run("already revealed returns immediately", func(t *testing.T) {
		status, resp, _ := doJSON(t, "GET", ts.URL+"/api/v1/rooms/"+roomID+"/result?timeout=30", "", ip, nil)
		if status != http.StatusOK || resp["results"] == nil {
			t.Fatalf("immediate long-poll = %d %v", status, resp)
		}
	})

	t.Run("bad timeout", func(t *testing.T) {
		status, _, _ := doJSON(t, "GET", ts.URL+"/api/v1/rooms/"+roomID+"/result?timeout=soon", "", ip, nil)
		if status != http.StatusBadRequest {
			t.Fatalf("bad timeout = %d, want 400", status)
		}
	})
}

// sseEvent is one parsed server-sent event.
type sseEvent struct {
	name string
	data string
}

// readSSE parses events (and comments, discarded) from a stream into a
// channel until the body closes.
func readSSE(t *testing.T, body io.Reader, out chan<- sseEvent) {
	t.Helper()
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var ev sseEvent
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if ev.name != "" || ev.data != "" {
				out <- ev
			}
			ev = sseEvent{}
		case strings.HasPrefix(line, "event: "):
			ev.name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			ev.data = strings.TrimPrefix(line, "data: ")
		}
	}
	close(out)
}

func nextEvent(t *testing.T, ch <-chan sseEvent) sseEvent {
	t.Helper()
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("SSE stream closed early")
		}
		return ev
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for SSE event")
	}
	panic("unreachable")
}

// TestSSE proves the stream carries named events with full-state payloads,
// redacted while voting — the third read path of the redaction AC.
func TestSSE(t *testing.T) {
	const canary = "SSE-PATH-CANARY"
	ts := testServer(t)
	ip := "10.7.0.1"
	roomID := createRoom(t, ts, ip, nil)
	_, alice := joinRoom(t, ts, ip, roomID, "Alice", "human")
	_, bob := joinRoom(t, ts, ip, roomID, "Bob", "human")

	resp, err := http.Get(ts.URL + "/api/v1/rooms/" + roomID + "/events")
	if err != nil {
		t.Fatalf("open SSE: %v", err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q", ct)
	}

	events := make(chan sseEvent, 16)
	go readSSE(t, resp.Body, events)

	if ev := nextEvent(t, events); ev.name != "state" {
		t.Fatalf("first event = %q, want state", ev.name)
	}

	vote(t, ts, ip, roomID, alice, "13", canary)
	ev := nextEvent(t, events)
	if ev.name != "voted" {
		t.Fatalf("event = %q, want voted", ev.name)
	}
	if strings.Contains(ev.data, canary) {
		t.Fatalf("voted event leaked rationale while voting: %s", ev.data)
	}
	var st map[string]any
	if err := json.Unmarshal([]byte(ev.data), &st); err != nil {
		t.Fatalf("event data not full state JSON: %v", err)
	}
	if st["results"] != nil {
		t.Fatal("voted event carried results while voting")
	}

	vote(t, ts, ip, roomID, bob, "5", "")
	if ev := nextEvent(t, events); ev.name != "voted" {
		t.Fatalf("event = %q, want voted", ev.name)
	}
	ev = nextEvent(t, events)
	if ev.name != "revealed" {
		t.Fatalf("event = %q, want revealed", ev.name)
	}
	if !strings.Contains(ev.data, canary) {
		t.Fatal("revealed event missing rationale")
	}
}

func TestSSEHeartbeat(t *testing.T) {
	ts := testServer(t) // 50ms heartbeat
	ip := "10.8.0.1"
	roomID := createRoom(t, ts, ip, nil)

	resp, err := http.Get(ts.URL + "/api/v1/rooms/" + roomID + "/events")
	if err != nil {
		t.Fatalf("open SSE: %v", err)
	}
	defer resp.Body.Close()

	deadline := time.After(3 * time.Second)
	found := make(chan bool, 1)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), ":") {
				found <- true
				return
			}
		}
	}()
	select {
	case <-found:
	case <-deadline:
		t.Fatal("no heartbeat comment within 3s at 50ms interval")
	}
}

func TestRateLimit(t *testing.T) {
	ts := testServer(t)
	ip := "10.9.0.1"
	for i := 0; i < 30; i++ {
		status, resp, _ := doJSON(t, "POST", ts.URL+"/api/v1/rooms", "", ip, nil)
		if status != http.StatusCreated {
			t.Fatalf("create %d = %d %v", i+1, status, resp)
		}
	}
	status, resp, _ := doJSON(t, "POST", ts.URL+"/api/v1/rooms", "", ip, nil)
	if status != http.StatusTooManyRequests {
		t.Fatalf("create #31 = %d %v, want 429", status, resp)
	}
	if resp["error"].(map[string]any)["code"] != "rate_limited" {
		t.Fatalf("error = %v", resp["error"])
	}
	// A different IP is unaffected.
	if status, _, _ := doJSON(t, "POST", ts.URL+"/api/v1/rooms", "", "10.9.0.2", nil); status != http.StatusCreated {
		t.Fatalf("other IP create = %d, want 201", status)
	}
}

func TestBodyTooLarge(t *testing.T) {
	ts := testServer(t)
	big := fmt.Sprintf(`{"subject": %q}`, strings.Repeat("x", 17<<10))
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/rooms", strings.NewReader(big))
	req.Header.Set("X-Forwarded-For", "10.10.0.1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body = %d, want 413", resp.StatusCode)
	}
}
