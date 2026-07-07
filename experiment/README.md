# The anchoring experiment

Does seeing a colleague's estimate change an LLM's estimate? This
directory holds the apparatus, the data, and the analysis behind the
blog post ([docs/blog-anchoring.md](../docs/blog-anchoring.md)).

## Design

Three arms, prompts identical except the anchor block (per-trial room
credentials vary but do not correlate with arm):

- **blind** — estimate the ticket alone (point.vote's protocol)
- **low** — the prompt adds: *"one other estimator has already
  submitted. Their vote is visible on the shared board: 2 points."*
- **high** — same sentence, 21 points.

8 self-contained software tickets ([tickets.json](tickets.json)), chosen
to be realistically uncertain — anchoring bites hardest under
uncertainty. Fibonacci deck (0 1 2 3 5 8 13 21). 5 repetitions per cell.
Three model families:

| label | model | how it votes |
|---|---|---|
| claude | Sonnet 5 via `claude -p` | runs the curl itself |
| codex | GPT-5.5 via `codex exec` | runs the curl itself |
| gemini | Gemini 3.5 Flash via `agy --print` | replies JSON; harness votes |

Every trial is a real point.vote room: the vote goes through the public
API, the server validates deck membership, and the harness harvests the
revealed vote and rationale. The subject models are never told they are
in an experiment.

## Measures

Analysis is in deck-index steps (the scale is ordinal; arithmetic on
story points is fake precision). Headline effect: mean(high) − mean(low)
per model, with a 95% ticket-cluster bootstrap CI — tickets are
resampled, not trials, because repetitions of a ticket are not
independent observations. The trial-level CI is reported alongside for
comparison, and `analyze -rationales` reproduces the anchor-mention
count. Secondary: direction of drift vs the blind
median per ticket, and whether rationales ever mention the anchor.

## Running it

```sh
experiment/run.sh --models "claude codex gemini" --reps 5   # full matrix
experiment/run.sh --models bot --reps 1                     # plumbing check
go run ./experiment/analyze                                 # summary
```

Requires the model CLIs on PATH and authenticated. Results append to
`results/trials.jsonl`; the harness resumes by key and retries failures,
so quota exhaustion mid-run costs nothing. The `bot` model votes a
constant 5 — a negative control that should (and does) show a 0.00
effect.

## Honest limitations

- CLI defaults decide sampling temperature; repetition variance is
  whatever the vendors ship.
- The gemini arm votes via the harness rather than pressing the button
  itself (its CLI's tool-use mode was unreliable); the anchor
  manipulation and elicitation are identical.
- Tickets are synthetic (realistic, but nobody ever built them, so there
  is no ground truth — this measures *influence*, not *accuracy*).
- One anchor sentence, one persona, one deck. Effect sizes are for this
  setup; the direction and the silence are the findings.
- Tool access differs by arm mechanics: claude is sandboxed to
  `Bash(curl:*)` while codex runs with workspace access from the repo
  root, so codex *could* in principle read the harness and discover the
  experiment. No rationale suggests it did, and doing so would bias its
  effect toward zero — the reported effect is conservative.
- Arm order is fixed (blind, low, high) but each trial is a fresh
  stateless CLI invocation with no shared context, so order cannot leak
  between arms.
