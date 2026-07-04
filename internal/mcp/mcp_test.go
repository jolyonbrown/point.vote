package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jolyonbrown/point.vote/internal/room"
)

// session spins up the /mcp endpoint over real streamable HTTP and returns
// a connected client session.
func session(t *testing.T) *sdk.ClientSession {
	t.Helper()
	svc := room.NewService(room.NewMemStore(), slog.New(slog.DiscardHandler))
	mux := http.NewServeMux()
	mux.Handle("/mcp", Handler(svc, slog.New(slog.DiscardHandler)))
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	client := sdk.NewClient(&sdk.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	cs, err := client.Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: ts.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// call invokes a tool and decodes its structured content into out.
func call(t *testing.T, cs *sdk.ClientSession, tool string, args map[string]any, out any) *sdk.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	if res.IsError {
		t.Fatalf("%s returned tool error: %s", tool, resultText(res))
	}
	if out != nil {
		raw, err := json.Marshal(res.StructuredContent)
		if err != nil {
			t.Fatalf("%s: marshal structured content: %v", tool, err)
		}
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("%s: decode structured content %s: %v", tool, raw, err)
		}
	}
	return res
}

func resultText(res *sdk.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// TestBodyCap pins the 16KB request-body limit on /mcp: oversized bodies
// must be rejected at the HTTP boundary, not parsed.
func TestBodyCap(t *testing.T) {
	svc := room.NewService(room.NewMemStore(), slog.New(slog.DiscardHandler))
	mux := http.NewServeMux()
	mux.Handle("/mcp", Handler(svc, slog.New(slog.DiscardHandler)))
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	big := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"pad":"` +
		strings.Repeat("x", 20<<10) + `"}}`
	req, _ := http.NewRequest("POST", ts.URL+"/mcp", strings.NewReader(big))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 400 || resp.StatusCode >= 500 {
		t.Fatalf("oversized /mcp body = %d, want 4xx", resp.StatusCode)
	}
}

// TestPublicHostHeader pins the production topology: cloudflared delivers
// requests to loopback with the public Host header, which the SDK's
// DNS-rebinding protection would otherwise 403. Regression for the launch
// outage where every MCP call through the tunnel was Forbidden.
func TestPublicHostHeader(t *testing.T) {
	svc := room.NewService(room.NewMemStore(), slog.New(slog.DiscardHandler))
	mux := http.NewServeMux()
	mux.Handle("/mcp", Handler(svc, slog.New(slog.DiscardHandler)))
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest("POST", ts.URL+"/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	req.Host = "point.vote" // loopback connection, public Host — the tunnel shape
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("ping with public Host = %d (%s), want 200", resp.StatusCode, body)
	}
}

func TestInstructionsAndToolList(t *testing.T) {
	cs := session(t)

	init := cs.InitializeResult()
	if !strings.Contains(init.Instructions, "the blindness is the point") {
		t.Fatalf("instructions missing the loop teaching: %q", init.Instructions)
	}

	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	want := map[string]bool{
		"create_room": false, "join_room": false, "cast_vote": false,
		"get_room": false, "reveal": false, "start_round": false,
		"wait_for_reveal": false,
	}
	for _, tool := range tools.Tools {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("tool %q not listed", name)
		}
	}
}

// TestFullLoop drives the whole §5 acceptance flow over MCP: create, join,
// blind vote (redacted), auto-reveal, wait_for_reveal, next round.
func TestFullLoop(t *testing.T) {
	const canary = "MCP-RATIONALE-CANARY"
	cs := session(t)

	var created createRoomResult
	call(t, cs, "create_room", map[string]any{"subject": "Pick the datastore",
		"custom_deck": []string{"postgres", "sqlite", "dynamo"}}, &created)
	if created.RoomID == "" || !strings.Contains(created.WebURL, "/r/"+created.RoomID) {
		t.Fatalf("create_room result = %+v", created)
	}

	var claude, gpt joinRoomResult
	call(t, cs, "join_room", map[string]any{"room_id": created.RoomID, "name": "claude"}, &claude)
	call(t, cs, "join_room", map[string]any{"room_id": created.RoomID, "name": "gpt", "kind": "agent"}, &gpt)
	if claude.Token == "" || gpt.Token == "" {
		t.Fatal("join_room returned empty tokens")
	}

	var accepted castVoteResult
	call(t, cs, "cast_vote", map[string]any{
		"room_id": created.RoomID, "token": claude.Token, "value": "sqlite", "rationale": canary,
	}, &accepted)
	if !accepted.Accepted {
		t.Fatal("vote not accepted")
	}

	// Redaction through the MCP read path: state while voting must carry
	// no values or rationales, in either structured or text content.
	var st room.State
	res := call(t, cs, "get_room", map[string]any{"room_id": created.RoomID}, &st)
	if st.Results != nil {
		t.Fatal("results non-nil while voting")
	}
	if st.Round.VotesCast != 1 {
		t.Fatalf("votes_cast = %d, want 1", st.Round.VotesCast)
	}
	raw, _ := json.Marshal(res)
	if strings.Contains(string(raw), canary) {
		t.Fatalf("get_room leaked rationale while voting: %s", raw)
	}
	// The default agent kind applied.
	for _, p := range st.Round.Participants {
		if p.Kind != room.KindAgent {
			t.Fatalf("participant %s kind = %q, want agent", p.Name, p.Kind)
		}
	}

	// Second vote completes the round; auto-reveal fires.
	call(t, cs, "cast_vote", map[string]any{
		"room_id": created.RoomID, "token": gpt.Token, "value": "postgres", "rationale": "boring is relative",
	}, nil)

	var revealed room.State
	call(t, cs, "wait_for_reveal", map[string]any{"room_id": created.RoomID, "timeout_s": 5}, &revealed)
	if revealed.Round.State != room.StateRevealed || revealed.Results == nil {
		t.Fatalf("wait_for_reveal state = %s", revealed.Round.State)
	}
	raw, _ = json.Marshal(revealed)
	if !strings.Contains(string(raw), canary) {
		t.Fatal("revealed state missing rationale")
	}
	if revealed.Results.Stats.Counts["sqlite"] != 1 || revealed.Results.Stats.Counts["postgres"] != 1 {
		t.Fatalf("counts = %v", revealed.Results.Stats.Counts)
	}

	var next room.State
	call(t, cs, "start_round", map[string]any{
		"room_id": created.RoomID, "token": claude.Token, "subject": "round two",
	}, &next)
	if next.Round.Seq != 2 || next.Round.State != room.StateVoting || len(next.History) != 1 {
		t.Fatalf("start_round state = seq %d %s, history %d", next.Round.Seq, next.Round.State, len(next.History))
	}
}

func TestManualRevealAndWaitTimeout(t *testing.T) {
	const canary = "WAIT-TIMEOUT-CANARY"
	cs := session(t)

	var created createRoomResult
	call(t, cs, "create_room", map[string]any{"auto_reveal": false}, &created)
	var p joinRoomResult
	call(t, cs, "join_room", map[string]any{"room_id": created.RoomID, "name": "solo"}, &p)
	call(t, cs, "cast_vote", map[string]any{"room_id": created.RoomID, "token": p.Token, "value": "5", "rationale": canary}, nil)

	// timeout_s 0 returns the current (voting) state immediately, like REST.
	start := time.Now()
	var st room.State
	call(t, cs, "wait_for_reveal", map[string]any{"room_id": created.RoomID, "timeout_s": 0}, &st)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("timeout_s 0 took %v, want immediate", elapsed)
	}
	if st.Round.State != room.StateVoting {
		t.Fatalf("state = %s, want voting", st.Round.State)
	}

	// auto_reveal off: wait_for_reveal times out, returning the voting
	// state — which must be redacted on this path too.
	start = time.Now()
	res := call(t, cs, "wait_for_reveal", map[string]any{"room_id": created.RoomID, "timeout_s": 1}, &st)
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Fatalf("wait_for_reveal returned in %v, should have blocked ~1s", elapsed)
	}
	if st.Round.State != room.StateVoting {
		t.Fatalf("state = %s, want voting", st.Round.State)
	}
	if raw, _ := json.Marshal(res); strings.Contains(string(raw), canary) {
		t.Fatalf("wait_for_reveal timeout path leaked rationale while voting: %s", raw)
	}

	var revealed room.State
	call(t, cs, "reveal", map[string]any{"room_id": created.RoomID, "token": p.Token}, &revealed)
	if revealed.Results == nil {
		t.Fatal("manual reveal returned no results")
	}
}

func TestToolErrors(t *testing.T) {
	cs := session(t)

	var created createRoomResult
	call(t, cs, "create_room", map[string]any{}, &created)
	var p joinRoomResult
	call(t, cs, "join_room", map[string]any{"room_id": created.RoomID, "name": "err-prober"}, &p)

	tests := []struct {
		name string
		tool string
		args map[string]any
		want string // substring of the error text
	}{
		{"unknown room", "get_room", map[string]any{"room_id": "no-such-room-00"}, "no such room"},
		{"bad token", "cast_vote", map[string]any{"room_id": created.RoomID, "token": "bogus", "value": "5"}, "token"},
		{"off-deck value", "cast_vote", map[string]any{"room_id": created.RoomID, "token": p.Token, "value": "4"}, "deck"},
		{"bad preset", "create_room", map[string]any{"deck": "tarot"}, "preset"},
		{"round while voting", "start_round", map[string]any{"room_id": created.RoomID, "token": p.Token}, "wrong state"},
		{"bad kind", "join_room", map[string]any{"room_id": created.RoomID, "name": "x", "kind": "ghost"}, "kind"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: tt.tool, Arguments: tt.args})
			if err != nil {
				t.Fatalf("transport error (want tool error): %v", err)
			}
			if !res.IsError {
				t.Fatalf("expected tool error, got success: %s", resultText(res))
			}
			if text := resultText(res); !strings.Contains(text, tt.want) {
				t.Fatalf("error text %q missing %q", text, tt.want)
			}
		})
	}
}
