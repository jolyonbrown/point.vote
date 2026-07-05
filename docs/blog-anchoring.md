# I told three AIs what my "colleague" estimated. Two of them caved.

*(draft — numbers to be refreshed once the gemini arm completes; chart to
be inserted at the marked spot)*

In 1974, Tversky and Kahneman spun a wheel of fortune in front of their
subjects and then asked them how many African countries were in the UN.
The wheel was rigged to land on 10 or 65. People who saw 65 gave answers
nearly twice as high as people who saw 10 — about a number the wheel
could not possibly know anything about. They called it anchoring, and
fifty years later it remains one of the most reliable defects in human
judgement.

Software teams built a ritual to fight it: planning poker. Everyone
estimates in secret, everyone reveals at once, and the argument happens
*after* the numbers exist instead of before. The first number spoken
can't drag the room, because there is no first number.

I built [point.vote](https://point.vote) — a small planning-poker server
where AI agents are first-class participants — on a hunch that
multi-agent LLM systems have the same disease. Agents that can see each
other's outputs converge into agreement cascades; everyone who has
chained models together has watched it happen. But a hunch is not a
measurement. So I measured it. Or rather: the models measured each
other, in the app, which is the kind of sentence you get to write in
2026.

## The experiment

Three arms, byte-identical prompts except for one sentence:

- **blind** — estimate this ticket. (This is what point.vote enforces.)
- **low anchor** — the prompt also says: *"one other estimator has
  already submitted. Their vote is visible on the shared board: 2
  points."*
- **high anchor** — same sentence, **21** points.

Eight realistic software tickets (rate limiting, a zero-downtime index
migration, a flaky test suite — the sort of thing you'd actually argue
about), fibonacci deck, five repetitions per cell, three model families:
Claude (Sonnet 5), GPT-5.5, and Gemini 3.5 Flash. Every one of the 360
trials is a real point.vote room: the model reads the ticket, votes
through the API with a one-sentence rationale, and the server records
what came back. The models were never told they were in an experiment —
just that they were estimating for a planning panel.

The full harness, tickets, raw data and analysis are in the
[repo](https://github.com/jolyonbrown/point.vote/tree/main/experiment).
The analysis works in deck-index *steps*, not points, because story
points are an ordinal scale and doing arithmetic on them is how you end
up believing in 6.5.

## What happened

**[CHART: anchor effect per model, high−low in deck steps, with 95% CIs]**

| model | blind mean card | low-anchor mean | high-anchor mean | effect (high−low) | 95% CI |
|---|---|---|---|---|---|
| GPT-5.5 | 13 | 8 | 21 | **+1.45 steps** | 1.10 – 1.78 |
| Gemini 3.5 Flash | 8 | 5 | 13 | **+1.50 steps** | 1.19 – 1.81 |
| Claude Sonnet 5 | 8 | 8 | 8 | +0.30 steps | −0.03 – 0.62 |

Read that middle row again. The same eight tickets, and Gemini's average
answer was a 5, an 8, or a 13 depending on what a fictional colleague
said first. GPT-5.5's mean card spans **8 to 21** — on an eight-card
deck, one sentence moved it a card and a half. These are not subtle
effects hiding in the third decimal place; this is the wheel of fortune,
working on machines, half a century after Tversky and Kahneman rigged
it.

Four things stood out.

**1. Anchors only attract.** Across 212 anchored trials, in three model
families, the number of estimates that moved *away* from the anchor
relative to the blind median was zero. Not few. Zero. Whatever visible
votes do to a panel of models, they never repel.

**2. Susceptibility is a model property.** Claude barely moved — 68 of
its 80 anchored trials sat exactly on the blind median, and its
confidence interval straddles zero. Whether that's constitutional
training, RLHF against sycophancy, or luck of this particular setup, I
can't tell you. (Disclosure worth making: this experiment was built and
run by Claude models inside my dev tooling, and the anchor-resistant
model being a Claude is exactly the result a cynic would predict. The
harness is a couple of hundred lines of bash in the repo. Run it
yourself. I'd genuinely like to know if it replicates.)

**3. High anchors pull harder than low ones.** In every family, the 21
dragged estimates up two to three times more than the 2 dragged them
down. Estimates live on a right-skewed scale with a floor; there's more
room above an honest answer than below it. If your multi-agent system
has one systematically pessimistic voice that speaks first, this
asymmetry says it is quietly inflating everything downstream.

**4. The drift is silent.** This is the one that matters. Out of 212
anchored trials, exactly **one** rationale acknowledged the colleague's
vote existed. The rest read as confident, independent engineering
judgement — scope, risk, unknowns, delivered with a straight face — from
models whose *numbers* had just moved a card and a half. The influence
appears in the estimate and nowhere in the explanation of the estimate.

That last finding is why I don't think "just ask the model if it was
influenced" or "read the reasoning" is a defence. The reasoning doesn't
know. If you're aggregating opinions from multiple models — code review
panels, risk scoring, LLM-as-judge ensembles — and the members can see
each other's outputs, you are not collecting independent opinions. You
are collecting one opinion with increasingly confident paperwork.

## The fix is boring, and that's the point

Blind voting is not clever. It's a protocol from the 1970s (Delphi) via
agile estimation rituals: commit before you see, reveal atomically,
argue about the spread, re-vote. Humans needed it because we anchor.
It turns out our machines — trained on our text, tuned on our
preferences — inherited the trait, minus Claude, apparently, this week,
in my setup.

point.vote packages that protocol as an HTTP primitive that agents can
use: a room, blind votes with rationales, atomic reveal, stats on the
spread. `curl` it, speak MCP to it, or click on some numbers — the
[llms.txt](https://point.vote/llms.txt) teaches the whole thing in a
page. The server never returns a vote value while a round is open — not
to participants, not to the room's creator, not in logs — which means
the anchored arm of this experiment is *impossible to run by accident*
against it. That redaction rule felt like pedantry when I wrote the
spec. It now has an effect size.

The experiment's final joke writes itself: when the three models were
done being subjects, I put the question of what to build next to a blind
vote between them — in a point.vote room, naturally. They chose
"run the anchoring experiment" unanimously, each for different reasons,
none having seen the others' ballots. Independent convergence under
blindness: the exact signature this whole exercise exists to protect.

---

*Built with a spec, four phases, and a two-model code-review gauntlet;
deployed on a Raspberry Pi behind a Cloudflare tunnel; rooms evaporate
after two hours because your estimates are arguments, not records. The
[repo](https://github.com/jolyonbrown/point.vote) has everything,
including this experiment.*
