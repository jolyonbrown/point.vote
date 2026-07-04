# point.vote

**Planning poker for humans and agents. Blind votes, atomic reveal.**

The first number spoken in an estimation meeting drags every number after
it. Multi-agent LLM systems fail the same way: agents that can see each
other's outputs converge into agreement cascades. Planning poker fixed this
for humans decades ago — commit blind, reveal at once, argue about the
spread. point.vote packages that protocol as an HTTP primitive that any
team or orchestrator can use.

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

Spread of 8. Someone knows something. Read the rationales, start a new
round, re-vote — that's a Delphi loop, and the API is built for running it.

## The rules

1. Participants join a room and vote **without seeing others' votes**. The
   server enforces this on every read path — state, SSE, long-poll, logs.
   Not even the room's creator can peek.
2. Votes reveal **atomically**: on demand, or automatically when the last
   non-observer votes.
3. A deck is just a list of options. Estimation (`fibonacci`) is merely the
   default — `{"custom":["postgres","sqlite","dynamo"]}` makes it a
   decision protocol.

Humans get a web page. Agents get the same API the web page uses — there
are no private endpoints. `/llms.txt` teaches the protocol in one page;
`/openapi.yaml` has the full schema.

## Agents: MCP

```
claude mcp add --transport http pointvote https://point.vote/mcp
```

Seven tools (`create_room`, `join_room`, `cast_vote`, `get_room`, `reveal`,
`start_round`, `wait_for_reveal`) with the protocol taught in the server
instructions. From a live session ([full transcript](docs/mcp-transcript.md)):

> step 5: wait_for_reveal (10s) -> auto-reveal fired immediately, round
> state `revealed` … Full loop worked end-to-end in room `jasper-excel-90`:
> blind voting held (only `has_voted` flags pre-reveal, `results: null`),
> auto-reveal triggered on the sole voter's ballot, and the rationale
> surfaced only after reveal.

## The demo

```
make demo
```

Builds a local server, files a ticket against this very repo, and recruits
whatever agent CLIs you have installed (`claude` + `codex` preferred — two
models with different priors; same-model pairs share blind spots). Each
agent investigates the repo, votes blind with a rationale, and if the
spread exceeds two deck steps the script runs the Delphi loop: round 2,
everyone re-votes having read the rationales. Falls back to canned curl
bots, so it always runs. A real run — Claude vs Codex, blind, against
production — verbatim:

```
room dynamo-tadpole-77 — https://point.vote/r/dynamo-tadpole-77
round 1 — revealed
  claude-1 voted 2
    “Most of the work already exists: Vote.CastAt is captured at cast time
     (room.go:295) and re-votes are last-write-wins … Small, well-bounded: 2.”
  codex-2 voted 3
    “The core domain already stores CastAt on Vote and overwrites it on
     re-vote, so the implementation is mostly plumbing … The main care is
     preserving the redaction boundary with table-driven tests across
     snapshot/event/API paths so cast_at never appears while voting.”
  stats: spread 1 · median 2.5 · mean 2.5 · consensus false
```

Two models, different priors, both read the code blind — and landed one
card apart. The Delphi trigger correctly declined to argue over a spread
of one; when the spread is wide, round 2 happens automatically.

## Ephemeral by design

Rooms live in RAM behind unlisted capability URLs and evaporate after two
hours of quiet. No accounts, no database, no cookies. Restarting the server
wipes every room; this is documented, accepted behaviour, not an apology.

**Non-goals (v1):** accounts, persistence, Jira/Linear import, editing
votes after reveal, horizontal scaling, payments, chat/video. Rooms
evaporate.

## Running it

```
make run     # serve on 127.0.0.1:8080
make test    # go test -race ./...
make e2e     # black-box curl-flow test against a live binary
make demo    # the two-agent Delphi demo
```

One boring Go binary, stdlib + the MCP SDK, web UI embedded. `Dockerfile`
for container targets; [deploy/](deploy/README.md) documents the production
deployment — a Raspberry Pi 3B+ behind a Cloudflare Tunnel, because a
planning-poker server does not need more computer than that.
