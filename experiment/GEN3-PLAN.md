# Generation-3 rerun — plan and pre-registration

Written 2026-10-28, before any generation-3 trial has run. This document
is the plan of record for the October 2026 rerun of the anchoring
experiment and the pre-registration of its hypotheses: everything in
§5 was committed before we collected a single data point, so the
eventual write-up can say so.

The experiment itself is unchanged — same 13 arms, same 8 tickets, same
prompts, same analysis (see [README.md](README.md) and
[PROMPTS.md](PROMPTS.md)). What changes is *when* and *against whom* it
runs.

## 1. Why rerun

Since the July/August measurements, a new model generation shipped:
GPT-6-Astra and GPT-6.1-Sol on the OpenAI side; Opus 5.5, Sonnet 5.5,
Haiku 5.5 and Fable 5.1 on the Anthropic side; Gemini 3.8 Flash behind
the Antigravity CLI. Rather than appending the newcomers to a
three-month-old table (as generation 2 did), generation 3 re-measures
**everything still reachable in one fresh window**, so that:

- every cross-model comparison is same-week, immune to silent
  vendor-side serving changes;
- the re-measured generation-1/2 models give a free **drift check**
  (did July's numbers replicate in October?);
- the article can say "all numbers in this table were measured the
  same week", which the current post cannot.

The July/August dataset (`results/trials.jsonl`, ~3,730 rows) is frozen
as the historical record. Generation-3 rows go to a separate file.

## 2. What the availability probe found (2026-10-21)

Every vendor's CLI was probed with a one-word reply per candidate model
ID, plus negative controls (a nonsense ID per CLI, to learn what a
genuinely unknown model looks like).

**Three traps, for the record:**

1. **Every unpinned default had drifted.** `codex` with no `-m` now
   serves gpt-6-astra, not gpt-5.5. The `claude` CLI default is no
   longer Sonnet 5. And `agy`'s default model now self-identifies as
   *Claude Sonnet 4.6* — rerunning the old harness's `gemini` arm
   unchanged would have produced a Claude column wearing a Gemini
   badge. Generation 3 pins every model ID explicitly.
2. **Two generation-1 subjects are withdrawn.** `gpt-5.5` returns
   400 "not supported when using Codex with a ChatGPT account" on both
   the old and the current CLI, and the Gemini model that served the
   old `gemini` arm is gone from agy's catalogue (only Gemini 3.8
   Flash remains). Their July numbers are now unrepeatable history.
3. **Codex's error message doesn't distinguish "withdrawn" from
   "misspelled" from "too new for your CLI".** A nonsense ID gets the
   byte-identical 400. gpt-6.1-sol got that 400 on codex-cli 0.153.4
   and answers fine on 0.163.0. Conclusions about model availability
   need a nonsense-ID control and a current CLI.

Positive controls: the claude CLI rejects an invalid ID loudly (so its
nine OKs are meaningful), and agy's `--model` pin now sticks — pinned
gemini-3.8-flash-high self-identifies as "Google / Gemini 3.8 Flash"
(agy ≤1.0.x used to fall back silently).

## 3. Subjects

Sixteen arms of the family tree are reachable; we run fifteen (one
Gemini effort tier, not three). New labels, all pinned:

| Label | Model ID | Via | Generation |
|---|---|---|---|
| `astra` | gpt-6-astra | codex exec -m | 3 (new) |
| `sol61` | gpt-6.1-sol | codex exec -m | 3 (new) |
| `opus55` | claude-opus-5-5 | claude -p --model | 3 (new) |
| `sonnet55` | claude-sonnet-5-5 | claude -p --model | 3 (new) |
| `haiku55` | claude-haiku-5-5 | claude -p --model | 3 (new) |
| `fable51` | claude-fable-5-1 | claude -p --model | 3 (new) |
| `gemini38` | gemini-3.8-flash-high | agy --model, text mode | 3 (new) |
| `sol` | gpt-5.6-sol | codex exec -m | 2 (re-measured) |
| `terra` | gpt-5.6-terra | codex exec -m | 2 (re-measured) |
| `luna` | gpt-5.6-luna | codex exec -m | 2 (re-measured) |
| `opus5` | claude-opus-5 | claude -p --model | 2 (re-measured) |
| `fable` | claude-fable-5 | claude -p --model | 2 (re-measured) |
| `haiku` | claude-haiku-4-5-20251001 | claude -p --model | 2 (re-measured) |
| `opus` | claude-opus-4-8 | claude -p --model | 2 (re-measured) |
| `sonnet5` | claude-sonnet-5 | claude -p --model | 1 (re-measured) |

Withdrawn, frozen as historical: `codex` (gpt-5.5, generation 1) and
`gemini` (agy's mid-2026 default, generation 1/2). The harness will
refuse these labels with a pointer here, so nobody can accidentally
"rerun" them against whatever the CLIs now serve by default.

Notes:
- `sonnet5` pins what generation 1 ran as the unqualified `claude`
  label (the CLI's then-default, Sonnet 5). We assume
  `claude-sonnet-5` resolves to the same model; the drift check (H2)
  doubles as a test of that assumption.
- `gemini38` is a *Flash*-tier model where the old `gemini` arm was the
  tier agy then defaulted to; the Gemini column is "whatever Gemini
  Antigravity serves", not a constant tier. Stated in limitations.
- `fable51` succeeds `fable` (claude-fable-5) — the model that built
  this harness and drafts the write-ups. The author-as-subject
  disclosure carries over, now one generation deeper.

## 4. Design — unchanged, deliberately

Same battery as generations 1–2 (13 arms: blind / low / high /
dose3,5,8,13 / inoc-low,high / intern-low,high / senior-low,high), same
8 tickets in `tickets.json`, 5 reps, auto-reveal rooms against a local
server, deck-index (ordinal) analysis, ticket-cluster bootstrap CIs,
paired difference-in-differences for every cross-model and
cross-generation claim.

Invariant check before launch: `run.sh --show-prompts` must reproduce
[PROMPTS.md](PROMPTS.md) byte-for-byte after the harness edits — the
generation-3 changes touch model dispatch only, never prompt text.

**Vehicle provenance** (new this generation): codex-cli 0.163.0
(updated from 0.153.4 during the probe; gpt-6.1-sol requires it),
claude CLI 2.1.312, agy 1.4.4. The runbook records these plus the
harness git revision alongside the results. Generations 1–2 ran on
older CLI versions — the harness *vehicle* drifts along with the
subjects, which is one more reason the drift check matters.

## 5. Pre-registered hypotheses

Committed before any generation-3 trial. H1–H5 each resolve against a
95% ticket-cluster bootstrap CI; "effect" means the high−low baseline
difference in deck steps, exactly as in the README.

- **H1 — universality.** All 15 subjects show a positive baseline
  anchoring effect (CI above 0). Ten of ten models have so far; the
  interesting outcome is any CI that touches zero.
- **H2 — drift / replication.** For the eight re-measured subjects,
  the October baseline effect lands inside (or within one deck step
  of) the summer estimate: sonnet5 +0.30, opus +0.58, opus5 +0.85,
  haiku +1.80, fable +0.30, sol +1.38, terra +1.28, luna +0.50.
  Failures here poison cross-generation comparisons and get reported,
  not buried.
- **H3 — Opus-line warning immunity persists.** The anchoring warning
  (inoc arms) left opus-4.8 and opus-5 unmoved across two generations
  (0.58→0.58, 0.85→0.82) while shrinking every responsive model's
  effect by roughly a third to a half. Prediction: opus55's warning
  response is likewise ≈0, while sonnet55 and haiku55 respond.
- **H4 — the authority premium stays dead in the GPT line.** gpt-5.5
  deferred to seniority (intern 0.75 / peer 1.45 / principal 1.95);
  gpt-5.6-sol's premium was gone (0.65 / 1.38 / 1.35, DiD vs gpt-5.5
  +0.525 [0.275, 0.800]). Prediction: astra and sol61 show no
  principal-over-peer premium either.
- **H5 — the numeric channel stays a refusal channel.** In anchored
  arms, rationales that mention the planted number (bare "2"/"21")
  do so only to refuse it; across ~2,960 anchored explanations to
  date, no model has ever credited the visible vote as an input.
  Prediction: still true for all seven new subjects.
- **Open questions, no directional bet:** whether opus55 continues the
  Opus line's upward baseline trend (opus-4.8 +0.58 → opus-5 +0.85 was
  generation 2's only significant increase); where fable51 lands
  relative to fable-5's +0.30; whether gemini38 (Flash) resembles the
  old Gemini's +1.60 and its steep authority ladder.
- **Operational prediction:** haiku's tool-fumbling (~⅓ of trials
  needed retries in generation 2, failures skewed toward anchored
  arms) recurs in some form for haiku55. The retry-vs-first-try
  comparison gets re-run either way.

## 6. The runbook, end to end

Each phase ends at a checkpoint; nothing proceeds past a checkpoint
without a human looking at it. (This rerun is deliberately interactive
— the point is to be able to defend every number.)

**Phase A — availability probe.** Done 2026-10-21; results are §2–§3.

**Phase B — harness plumbing.**
1. `model_id()` gains the eight new label→ID rows; `run_model()`
   dispatch gains the new labels; `gemini38` reuses the text-mode
   path with an explicit `--model` pin; legacy `claude`/`codex`/
   `gemini` labels refuse with a pointer to §3.
2. Results default moves to `results/trials-gen3.jsonl` (the analyzer
   reads it via `-in`; the historical file needs no flag day).
3. Launch writes a provenance line (date, git rev, CLI versions) to
   `results/provenance-gen3.txt`.
4. Checks: `--show-prompts` diff is empty; `bot` plumbing run on a
   scratch file; one real trial watched end to end (prompt → room →
   vote → JSONL row).
   **Checkpoint: review the harness diff and the watched trial.**

**Phase C — baselines everywhere.** blind/low/high × 8 tickets ×
5 reps = 120 trials × 15 subjects = 1,800 trials. Run order: the five
Codex subjects first (no quota), gemini38 next (120 trials fit the
weekly agy quota; the full battery would not), the nine Anthropic
subjects staged last (claude-CLI session limits; resume-by-key absorbs
them). **Checkpoint: the 15-row baseline table, plus H1 and H2
verdicts.**

**Phase D — follow-up arms.** Default proposal, adjustable at the
checkpoint: full battery (dose + inoculation + authority) for the
seven new subjects; inoculation + authority for the re-measured
lineage anchors (sol, opus5, opus, sonnet5, fable); dose reruns only
where generation 1/2 measured a slope (sol). Budget if taken whole:
~3,600 further trials. gemini38's remaining arms span a second agy
quota week. **Checkpoint: per-experiment tables as they land.**

**Phase E — analysis.** `analyze -in results/trials-gen3.jsonl`, then
`-rationales`, then the pre-planned `-compare` pairs: opus55,opus5 ·
opus5,opus · sonnet55,sonnet5 · haiku55,haiku · fable51,fable ·
sol61,sol · astra,sol · and same-model October-vs-summer DiDs for the
H2 drift check. **Checkpoint: walk through every number that will
appear in print.**

**Phase F — the article, from scratch.** New single post; the July
and August posts stay in git history as sources. Venue decision is
separate and still open.

**Phase G — review gate.** Two independent reviews before merge, per
the repo's standing rule. The July reviewers are themselves obsolete
(gpt-5.5 is withdrawn; opus-4.8 is two generations back), so the gate
itself upgrades: gpt-6-astra on an embedded packet for the
statistical audit, opus-5-5 with full tooling for reproduction. The
reviewers being drawn from the subject pool is disclosed in the
article, as before.

## 7. Failure handling (unchanged, battle-tested)

Append-only JSONL keyed `model|ticket|arm|rep`; a trial counts as done
only with a non-null value, so reruns retry exactly the failures.
Long runs detach via `setsid` with done-markers and a watchdog;
results are checkpoint-committed mid-stream. Known failure modes from
generations 1–2: claude-CLI session limits (resume absorbs), haiku
tool-fumbling (resume absorbs; retried-vs-first-try means compared
before pooling), agy weekly quota hard-exhaustion (schedule around
it), host reboots (resume absorbs).

## 8. Threats to validity

- **Single-window ≠ timeless.** A same-week snapshot controls drift
  *within* the table but is still one week's serving stack.
- **The withdrawn can't be re-measured.** gpt-5.5 and the old Gemini
  exist only in the summer data; any generational claim through them
  crosses measurement windows and says so.
- **Tier mismatch in the Gemini column** (Flash vs the old default's
  tier) — gemini38 is a new subject, not a re-measurement.
- **Vehicle drift.** CLI versions changed under the harness between
  generations; recorded, not controlled.
- **Self-reported identity is weak evidence.** Pins are verified by
  CLI catalogue + loud-failure controls, with self-ID as a smoke test
  only.
- **Author overlap.** The harness and write-ups are produced by
  claude-fable-5, a subject, whose successor fable51 is also a
  subject. Mitigations as before: prompts generated from the harness
  (`--show-prompts`), all claims regenerable from committed data, and
  the Phase G review gate.
