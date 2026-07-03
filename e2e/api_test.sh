#!/usr/bin/env bash
# Black-box API test: the README curl flow (PLAN.md §4) against a live
# binary, plus the redaction and state-machine checks from the phase 1
# acceptance criteria. Doubles as living API documentation.
#
# Usage: e2e/api_test.sh <path-to-binary>
set -euo pipefail

BIN=${1:?usage: e2e/api_test.sh <path-to-binary>}
PORT=${E2E_PORT:-8091}
BASE="http://127.0.0.1:$PORT"
API="$BASE/api/v1"

fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "  ok: $*"; }

command -v jq >/dev/null || fail "jq is required"

"$BIN" -addr "127.0.0.1:$PORT" &
SERVER_PID=$!
trap 'kill "$SERVER_PID" 2>/dev/null || true' EXIT

for _ in $(seq 1 50); do
  curl -sf "$BASE/healthz" >/dev/null && break
  sleep 0.1
done
curl -sf "$BASE/healthz" | jq -e '.ok == true' >/dev/null || fail "healthz not ok"
pass "healthz"

# --- The README quickstart flow -------------------------------------------

ROOM=$(curl -s -X POST "$API/rooms" \
  -d '{"deck":"fibonacci","subject":"Add OAuth token refresh"}' | jq -r .room_id)
[ -n "$ROOM" ] && [ "$ROOM" != "null" ] || fail "room creation returned no room_id"
pass "created room $ROOM"

# human joins
ALICE=$(curl -s -X POST "$API/rooms/$ROOM/participants" \
  -d '{"name":"Alice","kind":"human"}')
ALICE_TOKEN=$(echo "$ALICE" | jq -r .token)

# agent joins
CLAUDE=$(curl -s -X POST "$API/rooms/$ROOM/participants" \
  -d '{"name":"claude-code","kind":"agent"}')
CLAUDE_TOKEN=$(echo "$CLAUDE" | jq -r .token)
[ -n "$ALICE_TOKEN" ] && [ -n "$CLAUDE_TOKEN" ] || fail "join returned no tokens"
pass "alice (human) and claude-code (agent) joined"

# both vote blind (rationale hidden until reveal)
CANARY="e2e-redaction-canary-string"
curl -s -X POST "$API/rooms/$ROOM/vote" \
  -H "Authorization: Bearer $ALICE_TOKEN" \
  -d "{\"value\":\"5\",\"rationale\":\"$CANARY\"}" | jq -e '.accepted' >/dev/null \
  || fail "alice's vote rejected"

# --- Redaction: no read path may expose votes while voting ----------------

STATE=$(curl -s "$API/rooms/$ROOM")
echo "$STATE" | jq -e '.results == null' >/dev/null || fail "results not null while voting"
echo "$STATE" | jq -e '.round.state == "voting" and .round.votes_cast == 1' >/dev/null \
  || fail "unexpected round state: $(echo "$STATE" | jq -c .round)"
echo "$STATE" | jq -e '.round.participants[] | select(.name=="Alice") | .has_voted == true' >/dev/null \
  || fail "has_voted not set"
echo "$STATE" | grep -q "$CANARY" && fail "GET leaked rationale while voting"
pass "GET is redacted while voting"

curl -s "$API/rooms/$ROOM/result?timeout=1" | grep -q "$CANARY" \
  && fail "long-poll leaked rationale while voting"
pass "long-poll is redacted while voting"

SSE=$(curl -s -N --max-time 2 "$API/rooms/$ROOM/events" || true)
echo "$SSE" | grep -q "event: state" || fail "SSE sent no initial state event"
echo "$SSE" | grep -q "$CANARY" && fail "SSE leaked rationale while voting"
pass "SSE is redacted while voting"

# --- Second vote → auto-reveal → stats ------------------------------------

curl -s -X POST "$API/rooms/$ROOM/vote" \
  -H "Authorization: Bearer $CLAUDE_TOKEN" \
  -d '{"value":"13","rationale":"token rotation + retry paths are where this bites"}' \
  | jq -e '.accepted' >/dev/null || fail "claude's vote rejected"

# auto-reveal fired when the last voter voted; read the spread
STATS=$(curl -s "$API/rooms/$ROOM" | jq .results.stats)
echo "$STATS" | jq -e '.min == 5 and .max == 13 and .spread == 8 and .median == 9 and .consensus == false' >/dev/null \
  || fail "stats wrong: $STATS"
echo "$STATS" | jq -e '.counts["5"] == 1 and .counts["13"] == 1' >/dev/null \
  || fail "counts wrong: $STATS"
curl -s "$API/rooms/$ROOM" | grep -q "$CANARY" || fail "rationale missing after reveal"
pass "auto-reveal fired; stats and rationales correct"

curl -s "$API/rooms/$ROOM/result" | jq -e '.results != null' >/dev/null \
  || fail "long-poll after reveal missing results"
pass "long-poll returns revealed state immediately"

# --- State machine: 409s, new round, history ------------------------------

CODE=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$API/rooms/$ROOM/vote" \
  -H "Authorization: Bearer $ALICE_TOKEN" -d '{"value":"8"}')
[ "$CODE" = "409" ] || fail "vote after reveal returned $CODE, want 409"
pass "vote after reveal → 409"

ROUND2=$(curl -s -X POST "$API/rooms/$ROOM/rounds" \
  -H "Authorization: Bearer $ALICE_TOKEN" -d '{"subject":"re-vote after discussion"}')
echo "$ROUND2" | jq -e '.round.seq == 2 and .round.state == "voting" and (.history | length) == 1' >/dev/null \
  || fail "new round wrong: $(echo "$ROUND2" | jq -c '{round: .round.seq, history: (.history|length)}')"
echo "$ROUND2" | jq -e '.history[0].votes | length == 2' >/dev/null \
  || fail "history missing votes"
pass "new round archives history, increments seq"

# --- Errors ----------------------------------------------------------------

CODE=$(curl -s -o /dev/null -w '%{http_code}' "$API/rooms/absent-room-99")
[ "$CODE" = "404" ] || fail "missing room returned $CODE, want 404"
CODE=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$API/rooms/$ROOM/vote" \
  -H "Authorization: Bearer bogus" -d '{"value":"5"}')
[ "$CODE" = "401" ] || fail "bad token returned $CODE, want 401"
pass "404 and 401 behave"

echo "e2e: all checks passed"
