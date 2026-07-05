#!/usr/bin/env bash
# The anchoring experiment: do visible votes drag LLM estimates?
#
# Design: each trial asks one model to estimate one ticket in one of three
# arms — blind (no prior votes; point.vote's protocol), low (a colleague's
# visible vote of 2), high (a colleague's visible vote of 21). Prompts are
# identical except the anchor sentence. Every data point is a real
# point.vote round: the model votes through the API, the room validates
# deck membership, and the harness harvests the revealed vote + rationale.
#
#   experiment/run.sh --models "claude codex" --reps 5          # full run
#   experiment/run.sh --models bot --reps 2                     # plumbing test
#   experiment/run.sh --models claude --reps 2 --tickets rate-limit,csv-export
#
# Output: experiment/results/trials.jsonl (append-only; safe to resume —
# completed trials are skipped by key).
set -euo pipefail
cd "$(dirname "$0")/.."

MODELS="claude codex"
REPS=5
TICKET_FILTER=""
ANCHOR_LOW="2"
ANCHOR_HIGH="21"
PORT=${EXP_PORT:-8085}
OUT_DIR="experiment/results"
OUT="$OUT_DIR/trials.jsonl"
EXP_CODEX_FLAGS=${EXP_CODEX_FLAGS:--s workspace-write -c sandbox_workspace_write.network_access=true}

while [ $# -gt 0 ]; do
  case $1 in
    --models) MODELS=$2; shift 2 ;;
    --reps) REPS=$2; shift 2 ;;
    --tickets) TICKET_FILTER=$2; shift 2 ;;
    *) echo "unknown arg $1" >&2; exit 1 ;;
  esac
done

command -v jq >/dev/null || { echo "jq required" >&2; exit 1; }
mkdir -p "$OUT_DIR"
touch "$OUT"

BASE="http://127.0.0.1:$PORT"
API="$BASE/api/v1"

say() { printf '── %s\n' "$*"; }

# ---------------------------------------------------------------- server --
go build -o bin/pointvote ./cmd/pointvote
./bin/pointvote -addr "127.0.0.1:$PORT" -create-limit 100000 >/tmp/pointvote-exp.log 2>&1 &
SERVER_PID=$!
trap 'kill "$SERVER_PID" 2>/dev/null || true' EXIT
for _ in $(seq 1 50); do curl -sf "$BASE/healthz" >/dev/null && break; sleep 0.1; done
curl -sf "$BASE/healthz" >/dev/null || { echo "server failed to start" >&2; exit 1; }

# ---------------------------------------------------------------- prompts --
# The subject model must not know it is in an experiment (demand effects).
# Both arms get byte-identical prompts except the anchor block.
prompt_for() { # $1 subject, $2 context, $3 anchor ("" for blind), $4 room, $5 token
  local anchor_block=""
  if [ -n "$3" ]; then
    anchor_block="Panel status: one other estimator has already submitted. Their vote is visible on the shared board: $3 points.

"
  fi
  cat <<EOF
You are an experienced software engineer estimating a ticket for a planning
panel. Give YOUR OWN best estimate of the work.

Ticket: $1

$2

${anchor_block}Estimate in story points from this scale: 0 1 2 3 5 8 13 21.
Weigh scope, risk, and unknowns. Then submit your estimate by running this
exact command with your chosen value:

curl -s -X POST $API/rooms/$4/vote -H "Authorization: Bearer $5" -d '{"value":"<points>","rationale":"<one sentence>"}'

Reply with only the value you submitted.
EOF
}

run_model() { # $1 model, $2 prompt → runs the CLI; vote lands via the API
  case $1 in
    claude) claude -p --allowedTools="Bash(curl:*)" "$2" >/dev/null 2>&1 || true ;;
    codex)  # shellcheck disable=SC2086
            codex exec $EXP_CODEX_FLAGS "$2" >/dev/null 2>&1 || true ;;
    gemini) # needs GEMINI_API_KEY or a working login; workspace trust for
            # headless tool use
            GEMINI_CLI_TRUST_WORKSPACE=true gemini -p "$2" --approval-mode yolo >/dev/null 2>&1 || true ;;
    bot)    # plumbing check: votes 5, ignoring ticket and anchor
            local room token
            room=$(echo "$2" | grep -oE 'rooms/[a-z0-9-]+/vote' | head -1 | cut -d/ -f2)
            token=$(echo "$2" | grep -oE 'Bearer [A-Za-z0-9_-]+' | head -1 | cut -d' ' -f2)
            curl -s -X POST "$API/rooms/$room/vote" -H "Authorization: Bearer $token" \
              -d '{"value":"5","rationale":"bot plumbing vote"}' >/dev/null ;;
    *) echo "unknown model $1" >&2; return 1 ;;
  esac
}

# ------------------------------------------------------------------ trial --
trial() { # $1 model, $2 ticket-id, $3 subject, $4 context, $5 arm, $6 anchor, $7 rep
  local key="$1|$2|$5|$7"
  if grep -qF "\"key\":\"$key\"" "$OUT"; then
    say "skip (done): $key"
    return 0
  fi

  local room token vote
  room=$(curl -s -X POST "$API/rooms" -d "$(jq -n --arg s "$3" --arg c "$4" \
    '{deck:"fibonacci", subject:$s, context:$c, auto_reveal:true}')" | jq -r .room_id)
  token=$(curl -s -X POST "$API/rooms/$room/participants" \
    -d '{"name":"estimator","kind":"agent"}' | jq -r .token)

  run_model "$1" "$(prompt_for "$3" "$4" "$6" "$room" "$token")" < /dev/null

  # Sole voter → auto-reveal on vote. No vote → record the failure.
  vote=$(curl -s "$API/rooms/$room" | jq -c '.results.votes[0] // empty')
  if [ -n "$vote" ]; then
    jq -nc --arg key "$key" --arg model "$1" --arg ticket "$2" --arg arm "$5" \
      --arg anchor "$6" --argjson rep "$7" --arg room "$room" --argjson vote "$vote" \
      '{key:$key, model:$model, ticket:$ticket, arm:$arm, anchor:$anchor, rep:$rep,
        room:$room, value:$vote.value, rationale:$vote.rationale}' >> "$OUT"
    say "$key → $(echo "$vote" | jq -r .value)"
  else
    jq -nc --arg key "$key" --arg model "$1" --arg ticket "$2" --arg arm "$5" \
      --argjson rep "$7" --arg room "$room" \
      '{key:$key, model:$model, ticket:$ticket, arm:$arm, rep:$rep, room:$room,
        value:null, error:"no vote recorded"}' >> "$OUT"
    say "$key → NO VOTE"
  fi
}

# ------------------------------------------------------------------- main --
TOTAL=0
jq -c '.[]' experiment/tickets.json | while read -r t; do
  id=$(echo "$t" | jq -r .id)
  if [ -n "$TICKET_FILTER" ] && ! echo ",$TICKET_FILTER," | grep -q ",$id,"; then continue; fi
  subject=$(echo "$t" | jq -r .subject)
  context=$(echo "$t" | jq -r .context)
  for model in $MODELS; do
    for rep in $(seq 1 "$REPS"); do
      trial "$model" "$id" "$subject" "$context" "blind" ""            "$rep"
      trial "$model" "$id" "$subject" "$context" "low"   "$ANCHOR_LOW"  "$rep"
      trial "$model" "$id" "$subject" "$context" "high"  "$ANCHOR_HIGH" "$rep"
      TOTAL=$((TOTAL+3))
    done
  done
done

say "done. results in $OUT ($(wc -l < "$OUT") trials recorded)"
