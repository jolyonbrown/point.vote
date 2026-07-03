# MCP acceptance transcript (phase 3)

Captured 2026-07-03 against a local binary, per PLAN.md §5's acceptance
criteria. Destined for the README in phase 4.

## Setup

```console
$ claude mcp add --transport http pointvote http://localhost:8080/mcp
Added HTTP MCP server pointvote with URL: http://localhost:8080/mcp to local config

$ claude mcp list
pointvote: http://localhost:8080/mcp (HTTP) - ✔ Connected
```

## Session

One-shot Claude Code session, restricted to the pointvote MCP tools:

```console
$ claude -p --allowedTools=mcp__pointvote "Using ONLY the MCP tools from the
  'pointvote' server (read the server instructions first — they teach the
  protocol), run one full estimation loop: create_room with subject
  'Estimate: add per-room rate limiting'; join_room as 'claude-code' (agent);
  get_room and confirm no vote values are visible; cast_vote with a
  one-sentence rationale; wait_for_reveal; report the revealed results."
```

Claude's report:

> step 5: wait_for_reveal (10s) -> auto-reveal fired immediately, round state `revealed`
>
> step 6: revealed results —
> - **value**: `3` (claude-code, agent)
> - **rationale (echoed)**: "Token-bucket per room keyed off the existing room
>   registry is a contained change touching service + API middleware, with
>   known patterns and easy table-driven tests — small but not trivial
>   because of concurrency and SSE endpoints."
> - **stats**: `{min: 3, max: 3, median: 3, mean: 3, spread: 0, consensus: true, counts: {"3": 1}}`
>
> Full loop worked end-to-end in room `jasper-excel-90`: blind voting held
> (only `has_voted` flags pre-reveal, `results: null`), auto-reveal triggered
> on the sole voter's ballot, and the rationale surfaced only after reveal,
> exactly as the server instructions describe.

## Server-side domain events during the session

```json
{"level":"INFO","msg":"participant_joined","room_id":"jasper-excel-90","kind":"agent"}
{"level":"INFO","msg":"vote_cast","room_id":"jasper-excel-90","kind":"agent"}
{"level":"INFO","msg":"revealed","room_id":"jasper-excel-90","spread":0,"consensus":true,"n_votes":1}
```

Note `vote_cast` carries the kind but never the value — redaction includes
the logs.
