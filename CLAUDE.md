# CLAUDE.md — point.vote

Planning poker for humans and agents. Blind votes, atomic reveal.

## Ground rules for this repo

- **PLAN.md is the spec.** Read it before touching anything. Work phase by
  phase; each phase's acceptance criteria gate the next. Do not start a new
  phase while the previous one's AC fail.
- `go test -race ./...` before every commit. Commit at least once per phase,
  conventional messages. Decisions not covered by the plan: prefer the boring
  option and note it in the commit body.
- Dependency budget: the MCP SDK and stdlib. Justify anything else in one
  line in the commit message.
- Deploy target is a Raspberry Pi 3B+ with aarch64 userland — release builds
  are `CGO_ENABLED=0 GOOS=linux GOARCH=arm64`. The app binds 127.0.0.1:8080
  only; the Cloudflare Tunnel is the sole ingress.
- Key commands: `make run | test | e2e | demo | deploy-pi`.

## Picking the right models for workflows and subagents

Rankings, higher = better. Cost reflects what I actually pay (OpenAI has
really generous limits), not list price. Intelligence is how hard a problem
you can hand the model unsupervised. Taste covers UI/UX, code quality, API
design, and copy.

| Model    | Cost | Intelligence | Taste |
|----------|------|--------------|-------|
| gpt-5.5  | 9    | 8            | 5     |
| sonnet-5 | 5    | 5            | 7     |
| opus-4.8 | 4    | 7            | 8     |
| fable-5  | 2    | 9            | 9     |

How to apply:

- These are defaults, not limits. You have standing permission to override
  them: if a cheaper model's output doesn't meet the bar, rerun or redo the
  work with a smarter model without asking. Judge the output, not the price
  tag. Escalating costs less than shipping mediocre work.
- Cost is a tie-breaker only; when axes conflict for anything that ships,
  intelligence > taste > cost.
- Bulk/mechanical work (clear-spec implementation, data analysis,
  migrations): gpt-5.5 — it's effectively free.
- Anything user-facing (UI, copy, API design) needs taste ≥ 7.
- Reviews of plans/implementations: fable-5 or opus-4.8, optionally gpt-5.5
  as an extra independent perspective.
- Never use Haiku models.
- Mechanics: gpt-5.5 is only reachable through Codex, via the
  `codex:codex-rescue` subagent (Codex plugin for Claude Code);
  `~/.codex/config.toml` defaults to gpt-5.5.
- Use gpt-5.5 via `/codex:rescue` when you want Codex to:
  - investigate a bug
  - try a fix
  - continue a previous Codex task
  - take a faster or cheaper pass with a smaller model
  - It supports `--background`, `--wait`, `--resume`, and `--fresh`. If you
    omit `--resume` and `--fresh`, the plugin can offer to continue the
    latest rescue thread for this repo.
- Use gpt-5.5 via `/codex:transfer` to create a persistent Codex thread from
  the current Claude Code session; it prints a `codex resume <session-id>`
  command. Use it when you started a debugging or implementation
  conversation in Claude Code and want to continue that same context
  directly in Codex.
- Use gpt-5.5 via `/codex:status` to see running and recent Codex jobs for
  the current repository. Use it to:
  - check progress on background work
  - see the latest completed job
  - confirm whether a task is still running
- Use gpt-5.5 via `/codex:result` to show the final stored Codex output for
  a finished job. When available, it also includes the Codex session ID so
  you can reopen that run directly in Codex with `codex resume <session-id>`.
- Use gpt-5.5 via `/codex:cancel` to cancel an active background Codex job.
- Claude models (sonnet-5, opus-4.8, fable-5) run via the Agent/Workflow
  model parameter.

Using gpt-5.5 inside workflows and subagents via the Codex plugin for Claude
Code (the model parameter only takes Claude models, so use a wrapper):

- Spawn a thin Claude wrapper agent with `model: sonnet`, `effort: low`
  whose prompt instructs it to write a self-contained Codex prompt, run
  `codex exec` via Bash, and return the result verbatim — no paraphrase.
- Container caveats, learned the hard way: Codex's bwrap sandbox cannot
  initialise inside this Docker container, so `codex exec` can read but
  not write or run commands unless the user explicitly authorises
  `--dangerously-bypass-approvals-and-sandbox` (ask; never assume).
  Read-only tasks (reviews, audits) need no bypass — embed everything the
  task needs (spec excerpt + full diff) in one packet delivered via
  `--prompt-file` or a quoted heredoc; never shell-interpolate a prompt.

## Applying the policy to this repo

- Domain logic, API surface, MCP tools (`internal/room`, `internal/api`,
  `internal/mcp`): fable-5 or opus-4.8. This is the product; correctness
  (vote redaction, atomic reveal) and API taste both matter.
- Bulk/mechanical: table-driven test scaffolding, the embedded wordlist,
  `openapi.yaml` transcription, systemd/cloudflared config: gpt-5.5 via
  `/codex:rescue` — effectively free, clear specs.
- Web UI and microcopy (`web/`): taste ≥ 7, so opus-4.8 or fable-5. The
  landing-page curl one-liner is the positioning; don't hand it to a 5.
- Reviews before each phase gate: fable-5 or opus-4.8, with gpt-5.5 as the
  independent second opinion — different priors catching different blind
  spots.
- Dogfood clause: estimate disagreements between models about this
  codebase get settled in a point.vote room. Obviously. (Exercised: the
  roadmap vote in `reed-truck-84` chose the anchoring experiment,
  unanimously, blind.)

## Settling decisions in a point.vote room

Works from any repo, not just this one — the app is live at
https://point.vote (REST + MCP at `/mcp`; the whole protocol fits in
https://point.vote/llms.txt).

1. Create a room whose question carries the full context brief and whose
   custom deck is the option list (deck values are free-form strings).
2. Join one participant per model; hand each model its participant token
   inside its prompt, plus the room URL.
3. Each model votes blind with a one-line rationale. Reveal is atomic —
   the server never leaks a vote while the round is open, so anchoring
   (see `experiment/`) is structurally impossible.
4. Ties or wide spread: share the revealed rationales, open a new round,
   re-vote — Delphi style. `settle` records the final call.
5. Rooms evaporate after 2h: copy the revealed state into the issue or PR
   before walking away.
