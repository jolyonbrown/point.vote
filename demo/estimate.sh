#!/usr/bin/env bash
# Two-agent estimation demo with an automated Delphi loop (PLAN.md §9,
# phase 4). Zero config: builds the binary, starts a local server, recruits
# whatever agent CLIs are installed, and prints both rounds' stats.
#
#   make demo            # or: demo/estimate.sh
#   DEMO_AGENTS="bot bot" demo/estimate.sh   # force the canned-bot path
#
# Agent preference: claude + codex (two models, different priors — same-model
# pairs share blind spots), else two claude instances, else two canned curl
# bots so the demo always runs.
set -euo pipefail

cd "$(dirname "$0")/.."

PORT=${DEMO_PORT:-8098}
BASE="http://127.0.0.1:$PORT"
API="$BASE/api/v1"
AGENT_TIMEOUT=${DEMO_AGENT_TIMEOUT:-360}

bold() { printf '\033[1m%s\033[0m\n' "$*"; }
say() { printf '── %s\n' "$*"; }
fail() { echo "demo: $*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

have curl && have jq || fail "curl and jq are required"

# ---------------------------------------------------------------- server --
SERVER_PID=""
CHILD_PIDS=()
cleanup() {
  [ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null || true
  for pid in "${CHILD_PIDS[@]:-}"; do kill "$pid" 2>/dev/null || true; done
}
trap cleanup EXIT

if [ -n "${POINTVOTE_URL:-}" ]; then
  BASE=${POINTVOTE_URL%/}
  API="$BASE/api/v1"
  say "using existing server at $BASE"
else
  say "building and starting a local point.vote"
  command -v go >/dev/null || fail "go toolchain required (or point POINTVOTE_URL at a running server)"
  go build -o bin/pointvote ./cmd/pointvote
  ./bin/pointvote -addr "127.0.0.1:$PORT" >/tmp/pointvote-demo.log 2>&1 &
  SERVER_PID=$!
  for _ in $(seq 1 50); do curl -sf "$BASE/healthz" >/dev/null && break; sleep 0.1; done
fi
curl -sf "$BASE/healthz" >/dev/null || fail "no server at $BASE (see /tmp/pointvote-demo.log)"

# ---------------------------------------------------------------- ticket --
SUBJECT="Estimate: per-participant vote timestamps in revealed results"
CONTEXT="TICKET-142
When a round is revealed, results.votes[] should gain a cast_at timestamp
(RFC3339) so orchestrators can measure decision latency per participant.

Acceptance criteria:
- cast_at appears in revealed results and round summaries, never earlier
- re-votes keep the LAST cast time (last write wins already)
- openapi.yaml and llms.txt updated
- table-driven tests for the redaction boundary

The repository you are sitting in is the codebase in question. Estimate in
story points (fibonacci deck)."

ROOM=$(curl -s -X POST "$API/rooms" \
  -d "$(jq -n --arg s "$SUBJECT" --arg c "$CONTEXT" \
        '{deck:"fibonacci", subject:$s, context:$c}')" | jq -r .room_id)
[ -n "$ROOM" ] && [ "$ROOM" != "null" ] || fail "could not create room"
bold "room $ROOM — $BASE/r/$ROOM"

# The director observes and can force a reveal if an agent wanders off.
DIRECTOR=$(curl -s -X POST "$API/rooms/$ROOM/participants" \
  -d '{"name":"director","kind":"observer"}' | jq -r .token)

# --------------------------------------------------------------- casting --
if [ -n "${DEMO_AGENTS:-}" ]; then
  read -r -a AGENTS <<<"$DEMO_AGENTS"
  [ "${#AGENTS[@]}" -eq 2 ] || fail "DEMO_AGENTS needs exactly two entries, e.g. \"claude bot\""
elif have claude && have codex; then
  AGENTS=(claude codex)
elif have claude; then
  AGENTS=(claude claude)
else
  AGENTS=(bot bot)
fi
NAMES=("${AGENTS[0]}-1" "${AGENTS[1]}-2")
say "estimators: ${NAMES[0]} (${AGENTS[0]}) and ${NAMES[1]} (${AGENTS[1]})"

# Pre-join both estimators so round 2 can re-prompt the same participants
# (a fresh CLI invocation can't remember a token it was never handed).
TOKENS=()
for name in "${NAMES[@]}"; do
  TOKENS+=("$(curl -s -X POST "$API/rooms/$ROOM/participants" \
    -d "{\"name\":\"$name\",\"kind\":\"agent\"}" | jq -r .token)")
done

agent_prompt() { # $1 name, $2 token, $3 extra guidance
  cat <<EOF
You are "$1", an estimator in a planning-poker room. The blindness is the
point: do NOT discuss or reveal your vote to anyone before the reveal.

Protocol reference: curl -s $BASE/llms.txt
Room state:         curl -s $API/rooms/$ROOM
You are already joined; your bearer token is $2.

The repository in your current directory is the codebase the ticket refers
to. Read the round's subject and context from the room state, investigate
the repo briefly (a few files at most) to ground your estimate, then cast
your vote with a one-paragraph rationale:

curl -s -X POST $API/rooms/$ROOM/vote \\
  -H "Authorization: Bearer $2" \\
  -d '{"value":"<deck value>","rationale":"<one paragraph>"}'

$3
Then wait for the reveal: curl -s "$API/rooms/$ROOM/result?timeout=55"
Finally, state your vote and one sentence on the outcome.
EOF
}

launch_agent() { # $1 kind, $2 name, $3 token, $4 extra → runs in background
  local kind=$1 name=$2 token=$3 extra=$4 log="/tmp/pointvote-demo-$name-r$ROUND.log"
  case $kind in
    claude)
      claude -p --allowedTools="Bash(curl:*)" \
        "$(agent_prompt "$name" "$token" "$extra")" >"$log" 2>&1 &
      ;;
    codex)
      codex exec -s workspace-write \
        -c 'sandbox_workspace_write={"network_access":true}' \
        "$(agent_prompt "$name" "$token" "$extra")" >"$log" 2>&1 &
      ;;
    bot)
      bot_vote "$name" "$token" >"$log" 2>&1 &
      ;;
    *) fail "unknown agent kind $kind" ;;
  esac
  CHILD_PIDS+=($!)
}

# The canned bots keep the demo honest-but-runnable with no CLIs installed:
# they disagree in round 1 and converge in round 2, like colleagues do.
bot_vote() { # $1 name, $2 token
  local value rationale
  if [ "$ROUND" = 1 ]; then
    case $1 in
      *-1) value=3;  rationale="Bounded change: Vote already carries CastAt, so this is plumbing it through Results and summaries plus the doc updates." ;;
      *)   value=13; rationale="The redaction boundary is the product; touching the wire shape needs tests on every read path, and doc drift is where this bites." ;;
    esac
  else
    value=8
    rationale="$1 updating after reading the other rationale: meeting in the middle at 8."
  fi
  sleep 1
  curl -s -X POST "$API/rooms/$ROOM/vote" -H "Authorization: Bearer $2" \
    -d "$(jq -n --arg v "$value" --arg r "$rationale" '{value:$v, rationale:$r}')" >/dev/null
}

wait_for_reveal() {
  local deadline=$((SECONDS + AGENT_TIMEOUT)) state
  while [ $SECONDS -lt $deadline ]; do
    state=$(curl -s "$API/rooms/$ROOM/result?timeout=55" | jq -r .round.state)
    [ "$state" = "revealed" ] && return 0
  done
  say "estimators are dawdling; the director reveals what we have"
  curl -s -X POST "$API/rooms/$ROOM/reveal" -H "Authorization: Bearer $DIRECTOR" >/dev/null
}

print_round() { # $1 round json (full state), $2 label
  echo "$1" | jq -e '.results != null' >/dev/null \
    || fail "round never revealed — the server may have gone away"
  bold "$2"
  echo "$1" | jq -r '.results.votes[] | "  \(.name) voted \(.value)\n    “\(.rationale)”"'
  echo "$1" | jq -r '.results.stats |
    "  stats: spread \(.spread // "—") · median \(.median // "—") · mean \(.mean // "—") · consensus \(.consensus)"'
}

# deck-step distance between the lowest and highest NUMERIC votes —
# jokers ("?", "☕") don't count toward the spread, matching the stats.
spread_steps() { # $1 state json
  echo "$1" | jq -r '
    .deck as $deck |
    [.results.votes[].value
      | select(test("^-?[0-9]+([.][0-9]+)?$"))
      | . as $v | ($deck | index($v)) | select(. != null)] |
    if length == 0 then 0 else (max - min) end'
}

# ---------------------------------------------------------------- round 1 --
ROUND=1
bold "round 1 — blind votes"
launch_agent "${AGENTS[0]}" "${NAMES[0]}" "${TOKENS[0]}" ""
launch_agent "${AGENTS[1]}" "${NAMES[1]}" "${TOKENS[1]}" ""
wait_for_reveal
R1=$(curl -s "$API/rooms/$ROOM")
print_round "$R1" "round 1 — revealed"

# ------------------------------------------------------------ delphi loop --
STEPS=$(spread_steps "$R1")
if [ "$STEPS" -le 2 ]; then
  bold "spread within two deck steps — no argument worth having. Done."
  exit 0
fi

say "spread of $STEPS deck steps — someone knows something. Round 2."
RATIONALES=$(echo "$R1" | jq -r '.results.votes[] | "- \(.name) voted \(.value): \(.rationale)"')
curl -s -X POST "$API/rooms/$ROOM/rounds" -H "Authorization: Bearer $DIRECTOR" \
  -d "$(jq -n --arg s "Round 2: re-vote after reading rationales" \
        --arg c "$CONTEXT

Round 1 came out wide. The revealed rationales:
$RATIONALES" '{subject:$s, context:$c}')" >/dev/null

ROUND=2
EXTRA="This is round 2 of a Delphi loop. Round 1 was wide. Everyone's
revealed rationales are in the room context — read them, weigh them against
your own view, and re-vote. Moving is not losing; anchoring is."
launch_agent "${AGENTS[0]}" "${NAMES[0]}" "${TOKENS[0]}" "$EXTRA"
launch_agent "${AGENTS[1]}" "${NAMES[1]}" "${TOKENS[1]}" "$EXTRA"
wait_for_reveal
R2=$(curl -s "$API/rooms/$ROOM")
print_round "$R2" "round 2 — revealed"

# ---------------------------------------------------------------- finale --
bold "the Delphi loop, in numbers"
echo "$R1" | jq -r '.results.stats | "  round 1: spread \(.spread // "—"), median \(.median // "—")"'
echo "$R2" | jq -r '.results.stats | "  round 2: spread \(.spread // "—"), median \(.median // "—"), consensus \(.consensus)"'
