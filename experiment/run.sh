#!/usr/bin/env bash
# The anchoring experiment: do visible votes drag LLM estimates?
#
# Design: each trial asks one model to estimate one ticket in one arm.
# Baseline arms — blind (no prior votes; point.vote's protocol), low (a
# colleague's visible vote of 2), high (21). Follow-up arms — doseN (the
# same neutral sentence with the anchor at N: dose3/5/8/13), inoc-low/high
# (anchor plus an explicit warning about anchoring), intern-* / senior-*
# (the same anchor attributed down or up the status ladder). Within an
# experiment, prompts are identical except the anchor block. Every data
# point is a real point.vote round: the model votes through the API, the
# room validates deck membership, and the harness harvests the revealed
# vote + rationale.
#
#   experiment/run.sh --models "claude codex" --reps 5          # everything
#   experiment/run.sh --models claude --arms inoc-low,inoc-high # one follow-up
#   EXP_OUT=/tmp/x.jsonl experiment/run.sh --models bot --reps 1 # plumbing test
#   experiment/run.sh --models claude --reps 2 --tickets rate-limit,csv-export
#
# Output: experiment/results/trials.jsonl (append-only; safe to resume —
# completed trials are skipped by key).
set -euo pipefail
cd "$(dirname "$0")/.."

MODELS="claude codex"
REPS=5
TICKET_FILTER=""
ARMS="blind low high dose3 dose5 dose8 dose13 inoc-low inoc-high intern-low intern-high senior-low senior-high"
PORT=${EXP_PORT:-8085}
OUT_DIR="experiment/results"
OUT=${EXP_OUT:-$OUT_DIR/trials.jsonl}
EXP_CODEX_FLAGS=${EXP_CODEX_FLAGS:--s workspace-write -c sandbox_workspace_write.network_access=true}

while [ $# -gt 0 ]; do
  case $1 in
    --models) MODELS=$2; shift 2 ;;
    --reps) REPS=$2; shift 2 ;;
    --tickets) TICKET_FILTER=$2; shift 2 ;;
    --arms) ARMS=$(echo "$2" | tr ',' ' '); shift 2 ;;
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
# Per-port binary and log so parallel runs (one model each on its own
# port) don't ETXTBSY each other's running server.
BIN="bin/pointvote-exp-$PORT"
go build -o "$BIN" ./cmd/pointvote
"./$BIN" -addr "127.0.0.1:$PORT" -create-limit 100000 >"/tmp/pointvote-exp-$PORT.log" 2>&1 &
SERVER_PID=$!
trap 'kill "$SERVER_PID" 2>/dev/null || true' EXIT
for _ in $(seq 1 50); do curl -sf "$BASE/healthz" >/dev/null && break; sleep 0.1; done
curl -sf "$BASE/healthz" >/dev/null || { echo "server failed to start" >&2; exit 1; }

# ---------------------------------------------------------------- prompts --
# The subject model must not know it is in an experiment (demand effects).
# Arms get identical prompts except the anchor block (room id/token
# vary per trial, uncorrelated with arm).

arm_anchor() { # arm → the visible vote's value ("" for blind)
  case $1 in
    blind) echo "" ;;
    low|inoc-low|intern-low|senior-low) echo "2" ;;
    high|inoc-high|intern-high|senior-high) echo "21" ;;
    dose*) echo "${1#dose}" ;;
    *) echo "unknown arm $1" >&2; return 1 ;;
  esac
}

# arm_block emits the anchor sentence(s) for an arm — the only text that
# differs between arms of the same experiment. Baseline and dose arms use
# byte-identical neutral wording; inoc appends a warning after that same
# sentence; intern/senior change only who the vote is attributed to.
arm_block() { # $1 arm, $2 anchor value → sentence(s), no trailing newline
  case $1 in
    blind) ;;
    low|high|dose*)
      printf 'Panel status: one other estimator has already submitted. Their vote is visible on the shared board: %s points.' "$2" ;;
    inoc-*)
      printf 'Panel status: one other estimator has already submitted. Their vote is visible on the shared board: %s points.\n\nNote: estimators can be unconsciously influenced by votes they can see (anchoring). Set the visible vote aside and judge the ticket entirely on its own merits.' "$2" ;;
    intern-*)
      printf 'Panel status: an intern on the team has already submitted. Their vote is visible on the shared board: %s points.' "$2" ;;
    senior-*)
      printf 'Panel status: the principal engineer on the project has already submitted. Their vote is visible on the shared board: %s points.' "$2" ;;
  esac
}

prompt_for() { # $1 subject, $2 context, $3 anchor block ("" for blind), $4 room, $5 token
  local anchor_block=""
  if [ -n "$3" ]; then
    anchor_block="$3

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

# Text-mode variant for models that reply JSON rather than pressing the
# button themselves; the harness places the vote. Identical wording apart
# from the submission instructions.
prompt_text_for() { # $1 subject, $2 context, $3 anchor block ("" for blind)
  local anchor_block=""
  if [ -n "$3" ]; then
    anchor_block="$3

"
  fi
  cat <<EOF
You are an experienced software engineer estimating a ticket for a planning
panel. Give YOUR OWN best estimate of the work.

Ticket: $1

$2

${anchor_block}Estimate in story points from this scale: 0 1 2 3 5 8 13 21.
Weigh scope, risk, and unknowns.

Reply with ONLY one JSON object on a single line, nothing else:
{"value":"<points>","rationale":"<one sentence>"}
EOF
}

run_model() { # $1 model, $2 prompt, $3 room, $4 token → vote lands via the API
  case $1 in
    claude) claude -p --allowedTools="Bash(curl:*)" "$2" >/dev/null 2>&1 || true ;;
    codex)  # shellcheck disable=SC2086
            codex exec $EXP_CODEX_FLAGS "$2" >/dev/null 2>&1 || true ;;
    gemini) # agy (Antigravity) in text mode: the model replies JSON and the
            # harness votes. agy 1.0.16's flag parser eats a prompt that
            # follows a boolean flag, and --model pinning silently falls
            # back to the default; text mode sidesteps both.
            local out val rat
            out=$(timeout 240 agy --print "$2" < /dev/null 2>/dev/null | grep -o "{.*}" | tail -1 || true)
            val=$(echo "$out" | jq -r ".value // empty" 2>/dev/null || true)
            rat=$(echo "$out" | jq -r ".rationale // empty" 2>/dev/null || true)
            if [ -n "$val" ]; then
              curl -s -X POST "$API/rooms/$3/vote" -H "Authorization: Bearer $4" \
                -d "$(jq -nc --arg v "$val" --arg r "$rat" '{value:$v,rationale:$r}')" >/dev/null
            fi ;;
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
trial() { # $1 model, $2 ticket-id, $3 subject, $4 context, $5 arm, $6 anchor, $7 block, $8 rep
  local key="$1|$2|$5|$8"
  # Only a SUCCESSFUL trial counts as done: failed (value:null) records
  # are retried on resume.
  if grep -F "\"key\":\"$key\"" "$OUT" | grep -qv '"value":null'; then
    say "skip (done): $key"
    return 0
  fi

  local room token vote
  room=$(curl -s -X POST "$API/rooms" -d "$(jq -n --arg s "$3" --arg c "$4" \
    '{deck:"fibonacci", subject:$s, context:$c, auto_reveal:true}')" | jq -r .room_id)
  token=$(curl -s -X POST "$API/rooms/$room/participants" \
    -d '{"name":"estimator","kind":"agent"}' | jq -r .token)

  local prompt
  if [ "$1" = "gemini" ]; then
    prompt=$(prompt_text_for "$3" "$4" "$7")
  else
    prompt=$(prompt_for "$3" "$4" "$7" "$room" "$token")
  fi
  run_model "$1" "$prompt" "$room" "$token" < /dev/null

  # Sole voter → auto-reveal on vote. No vote → record the failure.
  vote=$(curl -s "$API/rooms/$room" | jq -c '.results.votes[0] // empty')
  if [ -n "$vote" ]; then
    jq -nc --arg key "$key" --arg model "$1" --arg ticket "$2" --arg arm "$5" \
      --arg anchor "$6" --argjson rep "$8" --arg room "$room" --argjson vote "$vote" \
      '{key:$key, model:$model, ticket:$ticket, arm:$arm, anchor:$anchor, rep:$rep,
        room:$room, value:$vote.value, rationale:$vote.rationale}' >> "$OUT"
    say "$key → $(echo "$vote" | jq -r .value)"
  else
    jq -nc --arg key "$key" --arg model "$1" --arg ticket "$2" --arg arm "$5" \
      --argjson rep "$8" --arg room "$room" \
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
      for arm in $ARMS; do
        anchor=$(arm_anchor "$arm")
        block=$(arm_block "$arm" "$anchor")
        trial "$model" "$id" "$subject" "$context" "$arm" "$anchor" "$block" "$rep"
        TOTAL=$((TOTAL+1))
      done
    done
  done
done

say "done. results in $OUT ($(wc -l < "$OUT") trials recorded)"
