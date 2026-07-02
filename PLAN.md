# point.vote — build plan

A free, ephemeral planning-poker app where **agents are first-class participants**.
Humans and LLM agents join the same rooms, use the same API, and follow the same
rules. Spiritual sibling of vote.poker ("Come and click on some numbers. It's
important.") — but robots are welcome.

This document is the executable spec. Work through the phases in order. Each
phase has acceptance criteria; do not start the next phase until they pass.

---

## 0. Why this exists (read before coding)

Planning poker is an anti-anchoring protocol: humans commit estimates blind
because the first number spoken drags everyone else's. Multi-agent LLM systems
have the same failure mode — agents that can see each other's outputs converge
into agreement cascades. point.vote packages blind-commit / atomic-reveal as an
HTTP primitive that any orchestrator (or human team) can use:

1. Participants join a room and vote **without seeing others' votes**.
2. Votes are revealed **atomically** (manually, or automatically when everyone
   has voted).
3. The spread is inspected. Wide spread = disagreement worth arguing about.
   Exchange rationales, start a new round, re-vote. (This is a Delphi loop.)

A "deck" is just a list of options, so the same mechanism covers:

- **Estimation**: deck = fibonacci, subject = "Implement OAuth token refresh"
- **Decisions**: deck = `["postgres","sqlite","dynamo"]`, subject = "Pick the datastore"

Estimation is merely the default deck. Do not special-case it.

---

## 1. Design principles (non-negotiable)

1. **One boring binary.** Go, stdlib-first, no web framework. Static assets
   embedded via `embed.FS`. `go build` produces the whole product.
2. **Agents are users.** The browser UI consumes the exact same public REST +
   SSE API that curl and agents use. If curl can't do something, the UI can't
   either. No private endpoints.
3. **Ephemeral by default.** Rooms live in RAM and die after an idle TTL. No
   accounts, no database, no cookies beyond localStorage for the user's own
   display name. Persistence is a future paid seam behind an interface, not a
   v1 feature.
4. **Anti-anchoring is the product.** The server never returns vote values
   while a round is in `voting` state — not to participants, not to the room
   creator, not in logs. Enforced server-side, tested explicitly.
5. **Legible to machines.** `/llms.txt`, `/openapi.yaml`, JSON everywhere,
   MCP endpoint at `/mcp`. An agent that has never seen this site should be
   able to participate within one page of reading.
6. **Structured logs from day one.** `log/slog` with the JSON handler. Logs
   are for machines that happen to be readable by humans.

## 2. Stack decisions (already made — don't relitigate)

| Concern        | Choice                                                   | Why |
|----------------|----------------------------------------------------------|-----|
| Language       | Latest stable Go (1.24+), `net/http` ServeMux routing    | One binary, method routing is in stdlib since 1.22 |
| Realtime       | **SSE** (`text/event-stream`), not WebSockets            | All mutations are REST POSTs, so bidirectional adds nothing; SSE is curl-able and EventSource is built into browsers |
| State          | In-memory store behind a `Store` interface               | Ephemeral by design; interface is the seam for a later SQLite-backed paid history tier |
| MCP            | `github.com/modelcontextprotocol/go-sdk`, streamable HTTP transport mounted at `/mcp` on the same binary | Official SDK; verify latest release/API shape before pinning |
| Frontend       | Two embedded static pages, vanilla JS, no build step     | `EventSource` + `fetch` is all this needs |
| Tests          | stdlib `testing`, table-driven, `-race` in CI; one bash e2e using curl + jq | The e2e script doubles as living API documentation |
| CI             | GitHub Actions: `go vet`, `go test -race ./...`, build   | |
| Container      | Multi-stage Dockerfile → distroless/static, CGO_ENABLED=0 | |

Dependency budget: the MCP SDK and (only if genuinely needed) one small helper.
Everything else is stdlib. Justify any addition in the commit message.

## 3. Domain model

```go
type Room struct {
    ID           string        // e.g. "mint-otter-42"
    Deck         []string      // 2–26 options, each ≤ 24 chars
    AutoReveal   bool          // default true
    Participants map[string]*Participant
    Round        *Round        // current round
    History      []RoundSummary // ring buffer, last 20, session-only
    CreatedAt    time.Time
    LastActive   time.Time     // touched by every request; used for TTL GC
}

type Participant struct {
    ID        string
    Name      string // 1–40 chars
    Kind      string // "human" | "agent" | "observer"
    TokenHash [32]byte // sha256 of bearer token; raw token never stored
    JoinedAt  time.Time
}

type Round struct {
    Seq       int
    Subject   string            // optional, ≤ 200 chars — the title
    Context   string            // optional, ≤ 4KB — ticket body / acceptance criteria / diff summary
    State     string            // "voting" | "revealed"
    Votes     map[string]Vote   // participantID → vote
    StartedAt time.Time
}

type Vote struct {
    Value     string // must be a member of room.Deck
    Rationale string // optional, ≤ 500 chars; hidden until reveal
    CastAt    time.Time
}
```

Rules:

- Only `human` and `agent` kinds may vote; `observer` watches (orchestrator pattern).
- Re-voting is allowed while `State == "voting"`; last write wins.
- While voting, room state exposes `has_voted: true/false` per participant —
  never values or rationales.
- `Subject` and `Context` are visible to everyone at all times. Blindness
  applies to answers, never to the question. The room carries the question;
  each participant (human or agent) brings its own private context — an agent
  sitting in a repo investigates locally before voting, exactly as a human
  estimates from their own mental model of the codebase.
- **Auto-reveal** (default on): when every non-observer has voted, the round
  flips to `revealed` atomically. Agent-only rooms need no one to click a button.
- Reveal computes stats: per-value counts; for values that parse as numbers:
  min / max / median / mean / spread (max−min), plus `consensus: bool`
  (all numeric votes equal). Non-numeric votes ("?", "☕") are counted but
  excluded from the maths.
- Starting a new round archives a `RoundSummary` (subject, per-participant
  value+rationale, stats) into the ring buffer and increments `Seq`.
- Anyone in the room may reveal early or start a new round. Rooms are unlisted
  capability URLs; that's the v1 trust model, same as vote.poker.

Deck presets:

```
fibonacci: ["0","1","2","3","5","8","13","21","?","☕"]   (default)
tshirt:    ["XS","S","M","L","XL","?"]
powers2:   ["1","2","4","8","16","?"]
yesno:     ["yes","no","abstain"]
```

IDs and tokens:

- Room ID: `word-word-NN` from an embedded ~2k-word safe list + 2 digits.
  Memorable, phone-typeable, regenerate on collision.
- Participant token: 128-bit random, base64url, returned once on join, sent as
  `Authorization: Bearer <token>`. Server stores only the SHA-256.

Limits (constants, enforced, tested): 100 participants/room, 10k rooms in
memory, room TTL 2h idle (GC sweep every minute), per-IP room creation 30/hour
(in-memory token bucket), request bodies ≤ 16KB.

## 4. HTTP API (v1)

Base path `/api/v1`. JSON in/out. Errors: `{"error":{"code":"...","message":"..."}}`
with correct status codes (400 validation, 401 bad token, 404 no such room,
409 wrong state — e.g. voting after reveal).

```
POST /api/v1/rooms
  body: {"deck": "fibonacci" | {"custom": ["a","b"]}, "subject"?: string,
         "context"?: string (≤4KB), "auto_reveal"?: bool}
  → 201 {"room_id","web_url","api_url","events_url","mcp_url"}
  Creating does NOT auto-join; the creator joins like everyone else.

POST /api/v1/rooms/{id}/participants
  body: {"name": string, "kind": "human"|"agent"|"observer"}
  → 201 {"participant_id","token"}

GET  /api/v1/rooms/{id}
  → 200 full room state, redacted while voting:
    {"room_id","deck","auto_reveal","round":{"seq","subject","context","state",
     "votes_cast": N, "participants":[{"id","name","kind","has_voted"}]},
     "results": null | {"votes":[{"name","kind","value","rationale"}],
                        "stats":{"min","max","median","mean","spread","consensus",
                                 "counts":{value:N}}},
     "history":[RoundSummary]}

POST /api/v1/rooms/{id}/vote            (auth)
  body: {"value": string, "rationale"?: string}
  → 200 {"accepted":true}   409 if round already revealed

POST /api/v1/rooms/{id}/reveal          (auth) → 200 room state
POST /api/v1/rooms/{id}/rounds          (auth) body {"subject"?, "context"?} → 201 room state
DELETE /api/v1/rooms/{id}/participants/self (auth) → 204

GET  /api/v1/rooms/{id}/result?timeout=30
  Long-poll: blocks until the current round is revealed or timeout (max 55s),
  then returns room state. Lets curl-only agents wait without parsing SSE.

GET  /api/v1/rooms/{id}/events
  SSE stream. Named events: joined, left, voted, revealed, round_started.
  Every event's data payload is the FULL redacted room state (rooms are small;
  snapshots beat diffs for correctness and make clients trivial).
  Heartbeat comment every 25s. Support Last-Event-ID naively (just resend state).

GET /healthz            → 200 {"ok":true,"rooms":N}
GET /openapi.yaml       → hand-written spec (phase 4)
GET /llms.txt           → terse API guide for agents (phase 4)
```

The curl quickstart (this exact flow is the e2e test and the top of the README):

```bash
ROOM=$(curl -s -X POST https://point.vote/api/v1/rooms \
  -d '{"deck":"fibonacci","subject":"Add OAuth token refresh"}' | jq -r .room_id)

# human joins
ALICE=$(curl -s -X POST https://point.vote/api/v1/rooms/$ROOM/participants \
  -d '{"name":"Alice","kind":"human"}')

# agent joins
CLAUDE=$(curl -s -X POST https://point.vote/api/v1/rooms/$ROOM/participants \
  -d '{"name":"claude-code","kind":"agent"}')

# both vote blind (rationale hidden until reveal)
curl -s -X POST https://point.vote/api/v1/rooms/$ROOM/vote \
  -H "Authorization: Bearer $(echo $ALICE | jq -r .token)" \
  -d '{"value":"5"}'
curl -s -X POST https://point.vote/api/v1/rooms/$ROOM/vote \
  -H "Authorization: Bearer $(echo $CLAUDE | jq -r .token)" \
  -d '{"value":"13","rationale":"token rotation + retry paths are where this bites"}'

# auto-reveal fired when the last voter voted; read the spread
curl -s https://point.vote/api/v1/rooms/$ROOM | jq .results.stats
```

## 5. MCP server (the party trick)

Mount a streamable-HTTP MCP server at `/mcp` on the same binary, using the
official Go SDK. Tools are thin wrappers over the same internal room service
the REST API uses — zero duplicated logic.

Tools:

```
create_room(deck?, subject?, auto_reveal?)   → room_id, web_url
join_room(room_id, name, kind="agent")       → participant_id, token
cast_vote(room_id, token, value, rationale?) → accepted
get_room(room_id)                            → redacted/full state (same as REST)
reveal(room_id, token)                       → room state
start_round(room_id, token, subject?)        → room state
wait_for_reveal(room_id, timeout_s=30)       → room state (long-poll, max 55s)
```

MCP server `instructions` field teaches the loop explicitly:

> Join the room, cast your vote WITH a rationale before seeing anyone else's,
> then wait_for_reveal. If the spread is wide, read the other rationales,
> discuss, and re-vote in a new round. Do not ask other participants what they
> voted before revealing — the blindness is the point.

Acceptance for this phase: `claude mcp add --transport http pointvote
http://localhost:8080/mcp` and a Claude Code session can create a room, join,
vote, and read revealed results end-to-end. Put the transcript in the README.

## 6. Web UI (embedded, no build step)

Two pages served from `embed.FS`:

**Landing (`/`)** — name, one-line pitch ("Planning poker for humans and
agents. Blind votes, atomic reveal."), deck picker, Start-a-room button, and —
prominently — the curl one-liner to create a room, plus links to /llms.txt and
the MCP snippet. The curl on the homepage IS the positioning.

**Room (`/r/{id}`)** — join modal (name in localStorage, kind defaults human),
card grid for the deck, participant list with voted-ticks and a glyph per kind
(👤 human / 🤖 agent / 👁 observer), reveal + next-round buttons, results panel
showing values, spread, consensus, and rationales. Copy-link button.
EventSource with reconnect + jittered backoff; on any event, re-render from the
full-state payload (no client-side state merging).

Style: system-ui stack, dark-mode via `prefers-color-scheme`, generous type,
one accent colour, dry British microcopy (e.g. empty room: "Nobody here yet.
Democracy awaits."). It should look intentional, not framework-default. Must
be comfortably usable on a phone — people vote from sofas.

## 7. Observability

`log/slog` JSON to stdout. Request log line per API call (method, path,
room_id, status, duration_ms, participant_kind where known). Domain events:
`room_created`, `participant_joined{kind}`, `vote_cast{kind}` (never the
value while voting), `revealed{spread, consensus, n_votes}`, `room_expired`.
No metrics endpoint in v1; the log stream is the telemetry.

## 8. Repo layout

```
point-vote/
├── cmd/pointvote/main.go        # flag parsing, wiring, serve
├── internal/room/               # domain: Room, Round, rules, stats
│   ├── room.go  room_test.go
│   ├── store.go                 # Store interface
│   └── memstore.go memstore_test.go
├── internal/api/                # REST handlers, SSE hub, middleware
├── internal/mcp/                # MCP tool definitions → room service
├── web/                         # index.html, room.html, app.js, style.css
├── demo/estimate.sh             # two-agent demo (see phase 4)
├── e2e/api_test.sh              # curl+jq black-box test
├── openapi.yaml
├── Dockerfile  Makefile  .github/workflows/ci.yml
└── README.md
```

Concurrency model: `Hub` holding `map[string]*Room` behind a `sync.RWMutex`;
each `Room` has its own `sync.Mutex`; SSE subscribers are buffered channels
with non-blocking send (slow consumer just misses a snapshot — the next event
carries full state anyway, which is why snapshots-not-diffs matters).
`go test -race` must stay clean throughout.

## 9. Phases

**Phase 0 — scaffold.** go.mod, layout above, Makefile (`run test lint e2e
demo docker`), CI workflow, slog wiring, `/healthz`.
✅ AC: CI green on push; `make run` serves healthz.

**Phase 1 — engine + REST + SSE.** Domain model, memstore, all `/api/v1`
endpoints, long-poll `result`, SSE hub, limits + TTL GC, validation.
Table-driven tests for: vote redaction while voting (the critical one),
auto-reveal trigger, re-vote last-write-wins, stats maths, deck validation,
TTL expiry, 409s. `e2e/api_test.sh` implements the README curl flow against a
live binary.
✅ AC: `go test -race ./...` and `make e2e` pass; redaction test proves values
are unobtainable pre-reveal via every read path (GET, SSE, long-poll).

**Phase 2 — web UI.** Landing + room pages per §6.
✅ AC: two browser tabs run a full session (join, vote, auto-reveal, next
round) with no console errors; usable at 375px width; a third tab joined as
observer sees ticks but no values until reveal.

**Phase 3 — MCP.** `/mcp` endpoint, tools in §5, instructions text.
✅ AC: the `claude mcp add` flow in §5 works end-to-end locally; transcript
captured for README.

**Phase 4 — agent demo, docs, ship.**
- `demo/estimate.sh`: creates a room with a subject AND a context blob (the
  "ticket"), then recruits whichever agent CLIs are installed — preference
  order: `claude -p` + `codex exec` (two different models with different
  priors is the point; same-model pairs share blind spots), else two `claude`
  instances, else two canned curl bots so the demo always runs. Each agent's
  prompt contains: the room API URL, a pointer to /llms.txt for the protocol,
  a note that the current repo is the task's subject and may be investigated,
  and instructions to join / read the round / vote with a one-paragraph
  rationale / wait_for_reveal. After reveal, if the numeric spread exceeds
  two adjacent deck steps, the script starts round 2 and re-prompts each
  agent with the others' revealed rationales for a re-vote — the Delphi loop,
  automated. Print both rounds' stats as the finale.
- `/llms.txt` (terse: what this is, the curl flow, the MCP URL, the rules).
- `openapi.yaml`, hand-written, matching implemented behaviour.
- Dockerfile (distroless), README written as the launch post: lead with the
  anchoring insight, the homepage curl, the MCP add command, the demo gif.
- Deploy target: a Raspberry Pi 3B+ (1GB, quad A53) behind a Cloudflare
  Tunnel. Artefacts to produce:
  - CI release job cross-compiling the binary: `CGO_ENABLED=0 GOOS=linux
    GOARCH=arm64` (the Pi's userland is confirmed aarch64).
  - `make deploy-pi`: scp the binary + `systemctl restart pointvote` over
    ssh (host from a `PI_HOST` variable). A restart wipes live rooms —
    documented, accepted behaviour.
  - `deploy/pointvote.service`: dedicated user via `DynamicUser=yes`,
    `Restart=always`, `MemoryMax=256M`, `ProtectSystem=strict`,
    `NoNewPrivileges=yes`. The app binds `127.0.0.1:8080` only — the tunnel
    is the sole ingress; nothing on the LAN or internet reaches it directly.
  - `deploy/cloudflared-config.yml` + setup notes: cloudflared as its own
    systemd service, tunnel route `point.vote → http://localhost:8080`,
    DNS on Cloudflare's free plan. TLS terminates at the edge; no ports
    opened, home IP unexposed, works behind CGNAT.
  - SD-card hygiene note: the app writes nothing (rooms live in RAM); cap
    journald with `SystemMaxUse=64M`. The 25s SSE heartbeats keep the edge
    from reaping idle streams; the 55s long-poll cap fits Cloudflare's
    ~100s proxy timeout by design.
  - The Dockerfile remains in the repo for non-Pi targets; the Pi path is
    a bare static binary on purpose. Sticky single-instance is fine —
    rooms are in RAM and that is a feature, not an apology.
✅ AC: fresh clone → `make demo` works with zero config; README quickstart
verified copy-paste clean.

## 10. Non-goals for v1 (write these in the README too)

No accounts, no persistence, no Jira/Linear import, no editing votes after
reveal, no horizontal scaling, no payments, no chat/video. Rooms evaporate.

Deliberate seams for a later paid tier: the `Store` interface (SQLite
implementation = persistent history), `RoundSummary` already being a
serialisable event record, and team-scoped room namespaces. Do not build any
of it now.

## 11. Stretch (only if v1 is done and boredom strikes)

- `Accept: text/plain` content negotiation on room GET → ASCII table for
  terminal dwellers.
- A downloadable Claude Skill (SKILL.md served at `/skill`) teaching the
  estimation loop.
- Observer reactions (peanut gallery mode).

## 12. Working agreement for Claude Code

Commit per phase minimum, conventional messages. Keep handlers thin — rules
live in `internal/room` and are unit-tested there. When a decision isn't
covered here, prefer the boring option and note it in the commit body. Do not
add dependencies beyond §2 without a one-line justification. Run
`go test -race ./...` before every commit.
