// Package mcp exposes the room service as MCP tools over streamable HTTP,
// mounted at /mcp on the same binary. Tools are thin wrappers over the
// exact service the REST API uses — zero duplicated logic (PLAN.md §5).
package mcp

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jolyonbrown/point.vote/internal/room"
)

// instructions teach connected agents the anti-anchoring loop (PLAN.md §5).
const instructions = `Join the room, cast your vote WITH a rationale before seeing anyone else's, then wait_for_reveal. If the spread is wide, read the other rationales, discuss, and re-vote in a new round. Do not ask other participants what they voted before revealing — the blindness is the point.`

// maxBodyBytes caps /mcp request bodies before the SDK parses them,
// mirroring the REST surface's 16KB limit. A tool call is a JSON-RPC
// envelope around args whose largest legal field is a 4KB context.
const maxBodyBytes = 16 << 10

// Handler returns the streamable-HTTP MCP endpoint. It runs stateless: no
// session bookkeeping, every call self-contained — ephemeral by default,
// like the rest of the product.
func Handler(svc *room.Service, log *slog.Logger) http.Handler {
	inner := sdk.NewStreamableHTTPHandler(func(r *http.Request) *sdk.Server {
		// A server per request is cheap (seven AddTool calls) and lets
		// create_room mint web URLs for the origin the caller actually used.
		return newServer(svc, originFrom(r), log)
	}, &sdk.StreamableHTTPOptions{
		Stateless: true,
		// The SDK's DNS-rebinding protection 403s loopback-received
		// requests whose Host isn't localhost — which is exactly how
		// production traffic arrives (cloudflared → 127.0.0.1 with
		// Host: point.vote). The protection defends local servers with
		// privileged access; this endpoint is deliberately public and
		// unauthenticated, so a rebinding page gains nothing it couldn't
		// get by calling point.vote directly.
		DisableLocalhostProtection: true,
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		inner.ServeHTTP(w, r)
	})
}

func originFrom(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

type createRoomArgs struct {
	Deck       string   `json:"deck,omitempty" jsonschema:"deck preset: fibonacci (default), tshirt, powers2, or yesno"`
	CustomDeck []string `json:"custom_deck,omitempty" jsonschema:"custom deck of 2-26 options; overrides deck. A deck is just a list of options, so decisions work too: [\"postgres\",\"sqlite\"]"`
	Subject    string   `json:"subject,omitempty" jsonschema:"what is being voted on (<=200 chars)"`
	Context    string   `json:"context,omitempty" jsonschema:"supporting context: ticket body, acceptance criteria, diff summary (<=4KB)"`
	AutoReveal *bool    `json:"auto_reveal,omitempty" jsonschema:"reveal automatically when every non-observer has voted (default true)"`
}

type createRoomResult struct {
	RoomID string `json:"room_id"`
	WebURL string `json:"web_url"`
}

type joinRoomArgs struct {
	RoomID string `json:"room_id"`
	Name   string `json:"name" jsonschema:"display name, 1-40 chars"`
	Kind   string `json:"kind,omitempty" jsonschema:"human, agent (default), or observer"`
}

type joinRoomResult struct {
	ParticipantID string `json:"participant_id"`
	Token         string `json:"token"`
}

type castVoteArgs struct {
	RoomID    string `json:"room_id"`
	Token     string `json:"token" jsonschema:"bearer token from join_room"`
	Value     string `json:"value" jsonschema:"must be one of the room's deck options"`
	Rationale string `json:"rationale,omitempty" jsonschema:"why you voted this way (<=500 chars); hidden until reveal, then shared"`
}

type castVoteResult struct {
	Accepted bool `json:"accepted"`
}

type getRoomArgs struct {
	RoomID string `json:"room_id"`
}

type revealArgs struct {
	RoomID string `json:"room_id"`
	Token  string `json:"token"`
}

type startRoundArgs struct {
	RoomID  string `json:"room_id"`
	Token   string `json:"token"`
	Subject string `json:"subject,omitempty"`
	Context string `json:"context,omitempty"`
}

type waitForRevealArgs struct {
	RoomID   string `json:"room_id"`
	TimeoutS *int   `json:"timeout_s,omitempty" jsonschema:"seconds to wait: default 30, max 55, 0 returns current state immediately"`
}

func newServer(svc *room.Service, origin string, log *slog.Logger) *sdk.Server {
	s := sdk.NewServer(&sdk.Implementation{
		Name:    "pointvote",
		Title:   "point.vote — planning poker for humans and agents",
		Version: "0.1.0",
	}, &sdk.ServerOptions{
		Instructions: instructions,
		Logger:       log,
	})

	sdk.AddTool(s, &sdk.Tool{
		Name:        "create_room",
		Description: "Create an ephemeral voting room. Creating does not join you; call join_room next.",
	}, func(ctx context.Context, req *sdk.CallToolRequest, a createRoomArgs) (*sdk.CallToolResult, any, error) {
		deck := a.CustomDeck
		if len(deck) == 0 {
			preset := a.Deck
			if preset == "" {
				preset = room.DefaultPreset
			}
			var err error
			if deck, err = room.ResolvePreset(preset); err != nil {
				return nil, nil, err
			}
		}
		autoReveal := a.AutoReveal == nil || *a.AutoReveal
		st, err := svc.CreateRoom(deck, a.Subject, a.Context, autoReveal)
		if err != nil {
			return nil, nil, err
		}
		return nil, createRoomResult{RoomID: st.RoomID, WebURL: origin + "/r/" + st.RoomID}, nil
	})

	sdk.AddTool(s, &sdk.Tool{
		Name:        "join_room",
		Description: "Join a room. Returns your participant token — keep it for cast_vote/reveal/start_round.",
	}, func(ctx context.Context, req *sdk.CallToolRequest, a joinRoomArgs) (*sdk.CallToolResult, any, error) {
		kind := a.Kind
		if kind == "" {
			kind = room.KindAgent
		}
		pid, token, err := svc.Join(a.RoomID, a.Name, kind)
		if err != nil {
			return nil, nil, err
		}
		return nil, joinRoomResult{ParticipantID: pid, Token: token}, nil
	})

	sdk.AddTool(s, &sdk.Tool{
		Name:        "cast_vote",
		Description: "Cast (or change) your blind vote for the current round. Include a rationale — it is hidden until reveal, then shared.",
	}, func(ctx context.Context, req *sdk.CallToolRequest, a castVoteArgs) (*sdk.CallToolResult, any, error) {
		if _, err := svc.CastVote(a.RoomID, a.Token, a.Value, a.Rationale); err != nil {
			return nil, nil, err
		}
		return nil, castVoteResult{Accepted: true}, nil
	})

	sdk.AddTool(s, &sdk.Tool{
		Name:        "get_room",
		Description: "Current room state. While the round is voting you see has_voted flags only — never values; results appear after reveal.",
	}, func(ctx context.Context, req *sdk.CallToolRequest, a getRoomArgs) (*sdk.CallToolResult, any, error) {
		st, err := svc.State(a.RoomID)
		if err != nil {
			return nil, nil, err
		}
		return nil, st, nil
	})

	sdk.AddTool(s, &sdk.Tool{
		Name:        "reveal",
		Description: "Reveal the current round now (any participant may). With auto_reveal on, rounds also reveal themselves when every non-observer has voted.",
	}, func(ctx context.Context, req *sdk.CallToolRequest, a revealArgs) (*sdk.CallToolResult, any, error) {
		st, err := svc.Reveal(a.RoomID, a.Token)
		if err != nil {
			return nil, nil, err
		}
		return nil, st, nil
	})

	sdk.AddTool(s, &sdk.Tool{
		Name:        "start_round",
		Description: "Archive the revealed round and open the next one — the re-vote half of the Delphi loop.",
	}, func(ctx context.Context, req *sdk.CallToolRequest, a startRoundArgs) (*sdk.CallToolResult, any, error) {
		st, err := svc.StartRound(a.RoomID, a.Token, a.Subject, a.Context)
		if err != nil {
			return nil, nil, err
		}
		return nil, st, nil
	})

	sdk.AddTool(s, &sdk.Tool{
		Name:        "wait_for_reveal",
		Description: "Block until the current round is revealed or the timeout passes, then return room state. Cast your vote first.",
	}, func(ctx context.Context, req *sdk.CallToolRequest, a waitForRevealArgs) (*sdk.CallToolResult, any, error) {
		timeout := 30
		if a.TimeoutS != nil {
			if *a.TimeoutS < 0 {
				return nil, nil, room.ValidationError("timeout_s must be non-negative")
			}
			timeout = min(*a.TimeoutS, 55) // 0 = return current state now, like REST
		}
		// In stateless streamable HTTP the SDK strips request-context
		// cancellation from tool handlers, so a client that disconnects
		// mid-wait does not abort this call; the 55s cap bounds the
		// stranded goroutine instead. (REST long-polls do abort.)
		st, err := svc.WaitForReveal(ctx, a.RoomID, time.Duration(timeout)*time.Second)
		if err != nil {
			return nil, nil, err
		}
		return nil, st, nil
	})

	return s
}
