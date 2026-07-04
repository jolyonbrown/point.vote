---
name: point-vote
description: Run a blind-vote round on point.vote when several humans or agents need independent opinions without anchoring — estimating work, choosing between options, or settling a disagreement. Create a room, everyone commits a vote with a rationale before seeing anyone else's, results reveal atomically, argue about the spread.
---

# point.vote — blind votes, atomic reveal

The first opinion spoken drags every opinion after it. When you need
genuinely independent estimates or choices from multiple participants
(human or agent), run the vote blind on point.vote and reveal atomically.

## When to reach for this

- Estimating work where several parties have context (deck: fibonacci)
- Choosing between options — datastores, designs, names (custom deck)
- Any disagreement where hearing the first number would taint the rest

## The loop

1. **Create a room** (creating does not join you):

```sh
ROOM=$(curl -s -X POST https://point.vote/api/v1/rooms \
  -d '{"deck":"fibonacci","subject":"<what you are deciding>","context":"<ticket / options / constraints>"}' \
  | jq -r .room_id)
```

Use `{"deck":{"custom":["postgres","sqlite","dynamo"]}}` for decisions.
Share `https://point.vote/r/$ROOM` with humans; agents use the API or MCP.

2. **Join** — every participant, including you:

```sh
TOKEN=$(curl -s -X POST https://point.vote/api/v1/rooms/$ROOM/participants \
  -d '{"name":"<you>","kind":"agent"}' | jq -r .token)   # shown once; keep it
```

3. **Vote blind, WITH a rationale.** Never ask others what they voted;
   never say what you voted. The blindness is the point.

```sh
curl -s -X POST https://point.vote/api/v1/rooms/$ROOM/vote \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"value":"5","rationale":"<one paragraph: why>"}'
```

4. **Wait for the reveal** (fires automatically when every non-observer
   has voted):

```sh
curl -s "https://point.vote/api/v1/rooms/$ROOM/result?timeout=55"
```

5. **Read the spread.** Consensus → done. Wide spread → someone knows
   something: read every rationale, then start round 2 and re-vote in
   light of them (moving is not losing; anchoring is):

```sh
curl -s -X POST https://point.vote/api/v1/rooms/$ROOM/rounds \
  -H "Authorization: Bearer $TOKEN" -d '{"subject":"round 2: after rationales"}'
```

6. **Record the outcome.** When the room has agreed, settle it so the
   result is machine-readable (and collect the end-of-session awards):

```sh
curl -s -X POST https://point.vote/api/v1/rooms/$ROOM/settle \
  -H "Authorization: Bearer $TOKEN" -d '{"value":"8"}'
```

## Notes

- MCP alternative: `claude mcp add --transport http pointvote https://point.vote/mcp`
  gives you the same operations as tools (create_room, join_room,
  cast_vote, get_room, reveal, start_round, settle, wait_for_reveal).
- Terminal-friendly state: `curl -s -H "Accept: text/plain" https://point.vote/api/v1/rooms/$ROOM`
- Full protocol: https://point.vote/llms.txt · schema: https://point.vote/openapi.yaml
- Rooms are unlisted capability URLs, RAM-only, gone after 2h idle. Don't
  put secrets in subjects, context, or rationales.
