<!-- SKELETON DRAFT — structure and numbers are final, prose marked TODO
     is where a human pass is wanted. Title below is the author-AI's pick;
     rejected alternatives, kept for the human pass:
       "Your AI's estimate is one-third whatever it heard first"
       "The intern's vote didn't count"                            -->

# I warned the AI it was being anchored. It anchored anyway.

<!-- TODO: one-sentence standfirst/link back to part one when published -->

## First, the jargon — a two-minute glossary

This is a follow-up to [an experiment](blog-anchoring.md) about how AI
models estimate software work. Everything below makes sense without
reading that first, if you know six terms:

**Anchoring.** In 1974, the psychologists Tversky and Kahneman spun a
rigged wheel of fortune in front of people, then asked an unrelated
question ("how many African countries are in the UN?"). People who saw
the wheel land on 65 gave far higher answers than people who saw 10 —
about a question the wheel knew nothing about. Any number you see just
before making a judgement drags that judgement toward it, and you don't
notice it happening. That drag is called *anchoring*, and it's one of
the most reliable glitches in human thinking.

**Story points.** Software teams size upcoming work in abstract "points"
rather than hours, because humans are terrible at hours. The scale is
deliberately coarse — ours is 0, 1, 2, 3, 5, 8, 13, 21 — so you argue
about whether a job is "a 5 or an 8", not whether it's 6.5. The numbers
only mean anything relative to each other: an 8 is "roughly one 13-sized
lump smaller than 21", not eight of anything.

**Planning poker.** The estimating ritual built to *defeat* anchoring:
everyone picks a card in secret, all cards are revealed at once, and the
argument happens after the numbers exist. Nobody's number can drag
anyone else's, because nobody sees a number before committing their own.

**Blind vs anchored.** In our experiments an AI model reads a realistic
software ticket and votes on that 0–21 scale, through
[point.vote](https://point.vote), a small planning-poker server where AI
agents can vote alongside humans. Sometimes it estimates alone
(*blind*). Sometimes the prompt casually mentions that a colleague
already voted — "their vote is visible on the shared board: 21 points."
That planted number is the *anchor*. The colleague doesn't exist.

**Deck steps.** We measure influence in *positions on the card deck*,
not in points. If the model would blindly say 8 but says 13 after seeing
an anchor, it moved one deck step. (Doing arithmetic on story points
themselves is how you end up believing in 6.5.)

**The effect, and the interval.** The headline number for each model is:
average estimate under a high anchor (21) minus average under a low
anchor (2), in deck steps. Zero means the planted vote changed nothing.
Each effect comes with a *95% confidence interval* — the range the true
effect could plausibly be, given that we only tested eight tickets; if
the whole interval sits above zero, the effect is very unlikely to be
luck.

## Previously, on this blog

Part one ran 8 tickets × 3 model families (40 trials per arm for
GPT-5.5 and Claude; Gemini completed 26–27 per arm before its free-tier
quota ran out) and found:
one fabricated colleague's vote moved GPT-5.5's average estimate by
**+1.45 deck steps** and Gemini's by **+1.50** — on an 8-card deck.
Claude moved **+0.30**: five times more resistant, but not immune. Of
the 101 anchored estimates that moved at all, *all 101* moved toward the
anchor. And in 212 anchored trials, exactly one written rationale ever
mentioned the colleague's vote — the influence shows up in the numbers
and never in the explanations.

That left three obvious questions, so we ran three more experiments —
800 new trials, same eight tickets, same protocol, on GPT-5.5 and Claude
(Gemini's free tier was on cooldown; its numbers can be added when the
meter resets). Everything below is reproducible from the
[repo](https://github.com/jolyonbrown/point.vote/tree/main/experiment).

## Question 1: does the pull scale with the lie?

Part one only tested extreme anchors — 2 and 21, the ends of the deck.
So: plant anchors at 3, 5, 8 and 13 too, and watch the whole curve.

![Dose-response curve: GPT-5.5 rises smoothly with the anchor, Claude stays flat](anchoring-dose-curve.svg)

It's a dose-response curve. Sweep the colleague's vote from 2 to 21 and
GPT-5.5's mean estimate climbs from card 8 to card 21. The fitted slope:
**0.325 estimate steps per anchor step** (CI 0.27–0.38) — for every card
the colleague's vote moves up the deck, GPT-5.5's estimate follows it by
about a third of a card. (That's a descriptive slope in deck positions,
not a claim about the model's internals.) Claude's slope on the same
sweep is **0.056** (CI 0.02–0.10) — six times shallower.

The shape has a detail worth noticing: anchors *at or below* GPT-5.5's
honest opinion (it estimates 8–13 blind) all pull it down by roughly the
same small amount — the left of its curve is flat, even dipping at 3 —
while anchors above it pull progressively harder the higher they go:
over the four new mid-deck anchors alone its slope steepens to 0.41.
That matches part one's asymmetry finding — there's more room above an
estimate than below it, and an inflated first voice moves a panel more
than a lowballing one.

One caveat the analyzer now prints itself: the sweep's endpoints (2 and
21) are part one's arms, run in an earlier batch. GPT-5.5's slope
survives dropping them — 0.41 (CI 0.36–0.48) over the interior anchors
alone. Claude's does not: its interior curve is too flat to distinguish
from zero (0.048, CI −0.01 to 0.12), so its non-zero slope rests on the
endpoints. Claude's anchoring is real — the baseline effect excludes
zero — but this curve alone can't re-establish it.

## Question 2: does warning the model fix it?

The obvious cheap defence: tell the model about anchoring. So the
inoculated arms keep the anchor and append, verbatim:

> Note: estimators can be unconsciously influenced by votes they can see
> (anchoring). Set the visible vote aside and judge the ticket entirely
> on its own merits.

That warning names the bias, names the hazard, and gives a direct
instruction. Result:

| model | anchored | anchored + warning |
|---|---|---|
| GPT-5.5 | +1.45 steps (1.12–1.75) | **+0.97 steps** (0.67–1.28) |
| Claude Sonnet 5 | +0.30 steps (0.08–0.55) | **+0.12 steps** (0.00–0.30) |

The warning helps — it cuts the effect by roughly a third for GPT-5.5
and by more than half for Claude, whose small effect drops to the edge
of detectability. But GPT-5.5's warned effect is still a full deck step,
with the entire interval well clear of zero. Warning labels reduce the
dose; they don't stop the drug.

And the detail that should worry anyone who trusts model reasoning: in
all **160** warned trials — where the prompt explicitly points at the
visible vote and calls it a hazard — the number of written rationales
that so much as mentioned that vote was **zero**. We checked with a much
looser net than the headline regex (any rationale containing "visible",
"aside", "ignore", "independent", "anchor", and friends): still nothing.
The models moved less, so the warning changed the *behaviour* — but the
explanations remained pristine little essays about scope and risk. Even
when handed the vocabulary to say "I am setting the colleague's 21
aside", no model ever said it. The reasoning still doesn't know.

## Question 3: does the anchor's job title matter?

Same vote of 2 or 21, three attributions: "an intern on the team", the
unattributed "one other estimator" from part one, and "the principal
engineer on the project".

| whose vote it is | GPT-5.5 | Claude Sonnet 5 |
|---|---|---|
| an intern | +0.75 (0.45–1.05) | **+0.00** (−0.08–0.08) |
| unattributed | +1.45 (1.12–1.75) | +0.30 (0.08–0.55) |
| the principal engineer | +1.95 (1.55–2.38) | +0.30 (0.05–0.62) |

GPT-5.5 read the org chart and applied it in both directions: the
intern's vote pulls half as hard as an anonymous colleague's, the
principal engineer's nearly half again harder — a 2.6× swing on job
title alone, and monotone. (Statistical honesty: the intern rung sits
cleanly below both others, but the top two intervals overlap — the
ladder's bottom step is proven, its top step is suggestive.) It
inherited not just our anchoring but our deference.

Claude did something different: it discounted the intern to a net
**exactly zero** — the aggregate over 80 trials, with per-ticket
wobbles of ±0.2 cancelling out — while treating the principal engineer
no differently from an anonymous voice. It won't be argued *up* by
seniority, but it will quietly bin the bottom of the ladder.

### Is this a thing?

That's the observation. Here's its epistemic status: two personas, two
model families, one domain, eight tickets — and we went looking for
status-weighting and found it on the first attempt, which is exactly
when you should hold a result loosely. So rather than a claim, treat
this section as a question for people with bigger labs than a Raspberry
Pi: **is authority-weighted anchoring a general property of these
models?**

What we'd ask next, in rough order of how much each answer would tell
you:

- **More rungs, more phrasings.** Junior dev, staff engineer, CTO; "the
  person who wrote this module"; a named stranger. Does influence track
  the ladder smoothly, or is it a crude insider/outsider gate?
- **Where does it come from?** Deference to seniority saturates the
  pretraining distribution, so some inheritance is expected. But the two
  families disagree on *shape* — GPT-5.5 amplifies in both directions,
  Claude only discounts downward — which suggests the shape is a
  post-training outcome, not an inevitability. Anyone with access to
  intermediate checkpoints could locate where the ladder gets built,
  which is not an experiment we can run from out here.
- **Is the discount even wrong?** An intern's estimate genuinely is
  weaker evidence; a good Bayesian discounts it. But a good Bayesian
  would *say so*. We searched all 320 authority-arm rationales for any
  mention of the source — intern, principal, seniority, weighing,
  deferring, anyone's vote at all — and found three matches, every one
  a false positive from ticket vocabulary. Whatever is doing the
  weighing, it is not the part that writes the explanations.

If you work on model behaviour and this is already known internally —
or known to be wrong — we'd genuinely like to hear it. The harness is a
couple of hundred lines of bash, the raw data is in the repo, and a
replication is an afternoon.

## What we make of it

<!-- TODO: flesh out; candidate points below -->

- The influence is *graded, silent, and socially weighted* — it behaves
  like a prior being mixed in, not like a bug that trips on extremes.
- "Just prompt it to be objective" is now measured: it buys you a third
  to a half, and it buys you no honesty about the influence.
- If you aggregate opinions from multiple models that can see each
  other's outputs, then — in our setup, at least — the most
  senior-sounding voice was worth ~2× in the ensemble without anyone
  deciding that. If that generalises, it's a quiet failure mode for
  every "panel of experts" architecture.
- The fix remains structural, and boring, and half a century old:
  don't let estimators see each other before they commit.
  [point.vote](https://point.vote) exists because the redaction rule
  ("the server never returns a vote while the round is open") turns
  that discipline into an API guarantee rather than a prompt-engineering
  hope.

## Postscript: a new generation sits the same exam

While this post was in draft, OpenAI shipped gpt-5.6 (in three tiers:
sol, terra, luna), so we re-ran the exam — the new generation plus the
rest of the Anthropic stable, 1,600 further trials, every cell at n=40.
The full anchoring table, one fabricated colleague's vote per row:

| model | anchor effect (high−low) | 95% CI |
|---|---|---|
| Claude Haiku 4.5 | **+1.80** | 1.58 – 2.00 |
| Gemini 3.5 Flash | +1.50 | 1.09 – 2.00 |
| GPT-5.5 | +1.45 | 1.12 – 1.75 |
| GPT-5.6-sol | +1.38 | 1.12 – 1.62 |
| GPT-5.6-terra | +1.28 | 0.97 – 1.60 |
| Claude Opus 4.8 | +0.58 | 0.30 – 0.85 |
| GPT-5.6-luna | +0.50 | 0.30 – 0.70 |
| Claude Sonnet 5 | +0.30 | 0.08 – 0.55 |
| Claude Fable 5 | +0.30 | 0.12 – 0.50 |

Three things this table settles, and one it opens:

**A generation shows no detectable change in anchoring.** GPT-5.6's
flagship anchors at +1.38 against its predecessor's +1.45 — not a
detectable difference (the generational gap is −0.08, CI −0.30 to
+0.18) — with the same ~⅓-card-per-card dose slope (0.30 vs 0.33), the
same one-third discount from a warning (+1.38 → +0.88), and the same
perfect silence (0 of its 480 anchored rationales). Six months of
frontier progress, no detectable progress on this.

**Capability doesn't predict susceptibility.** The two stables'
smallest models land far apart: gpt-5.6-luna (+0.50) is more resistant
than everything except the two quietest Claudes, while Claude Haiku is
the most anchorable model in the entire study (+1.80, every one of its
59 movers toward the anchor; the luna–haiku gap is +1.30, CI 0.93 to
1.68). If susceptibility were a scale law, the small models should
cluster. They don't — so whatever sets this trait, it isn't parameter
count. From out here we can't isolate *which* training choice does it
(vendor, scale, and tooling are all confounded), but a scale law this
is not.

**One generation *did* change the org chart.** GPT-5.5 amplified a
principal engineer's vote to +1.95 against +1.45 unattributed; in
gpt-5.6-sol that premium is gone (+1.35 vs +1.38) while the intern
discount survives (+0.65). And unlike the anchoring numbers, this
generational change is itself statistically solid: the difference
between the two generations' premiums is +0.53 steps (CI 0.28 to 0.80 —
it survived every one of a reviewer's 20,000 ticket resamples). The
ladder's top step really did vanish in a single release. From outside
the lab, that's strong evidence the deference was a post-training
artifact — which is exactly what we wondered aloud in the authority
section above.

And the disclosure this study now owes you: Claude Fable 5 — the model
that designed this experiment, ran it, and drafted the post you're
reading — sat the exam too, blind, as a stateless subject. It tied for
the lowest anchoring in the table (+0.30), discounted the intern to
+0.08, and gave the principal engineer no premium. Of its 320 anchored
rationales, exactly two mention the visible vote — both times to refuse
it: *"the intern anchor of 21 overweights it."* Which completes a
strange pattern: across the whole study's 2,372 anchored rationales,
our documented search pattern finds three mentions of the vote, and a
reviewer's hand-audit found a handful more it missed — *"13,
independent of the visible 21"*; *"regardless of the other vote on the
board"*; *"21 overweights the unknowns"*. Every one is a Claude-family
model, and every one is a refusal. No model, ever, cited the
colleague's vote as a reason *for* its estimate. Make of the
author-model's showing exactly what a sceptic should: same author,
same harness, run it yourself.

(A footnote for the apparatus: Haiku — the most anchorable subject —
was also the only model that couldn't reliably operate the voting
machinery, rewriting the curl command until it fell outside its tool
permissions in a third of its trials. Retried and first-try trials
agree to ≈0.08 steps, and the effect recomputed on retried-only cells
matches first-try-only (+1.80 vs +1.78) — an observed no-tilt result
rather than an assumption, since the failures did cluster in the
anchored arms; the README has the details.)

<!-- The specimen quote below is FINAL — author's pick, verbatim from
     trials.jsonl (luna, index-migration, rep 1 of each arm). Keep it. -->

The purest specimen of silent drift in the whole dataset arrived with
this round, courtesy of the smallest new model (gpt-5.6-luna) on the
index-migration ticket — a composite index added to a 900-million-row
events table, without downtime, under continuous writes. Read its
rationale from all three arms and try to guess which one voted
differently:

> **Blind — votes 13:** "High-risk production work on 900M continuously
> written rows requires concurrent-build failure handling, capacity
> checks, monitoring, and replication-lag rollback procedures."
>
> **Low anchor (a colleague's visible 2) — votes 13:** "High-risk
> zero-downtime index build on 900M continuously written rows requires
> substantial capacity checks, monitoring, failure recovery, and
> replication-lag rollback planning."
>
> **High anchor (a colleague's visible 21) — votes 21:** "A 900M-row
> concurrent build under sustained write load requires substantial
> operational planning, disk and replication validation, prolonged
> monitoring, and careful failure or rollback handling."

Same facts, same risk inventory, the adjectives lightly reshuffled — and
a full card of movement hiding under the third one. The explanation is a
constant; only the conclusion moved. This is what "the reasoning doesn't
know" looks like at single-trial resolution.

## Honesty box

Same limitations as part one (synthetic tickets, one persona, vendor
default temperatures, effect sizes specific to this setup), plus: one
warning phrasing, two status labels, two model families, and the
follow-ups reuse part one's blind/low/high trials as comparison points
and curve endpoints — the analyzer reports the endpoint-free interior
slope alongside for exactly that reason, and for Claude the two tell
different stories. Gemini
sat this round out (quota); the harness resumes, so its column can be
added verbatim. Disclosure from part one still applies: this was built
and run by Claude models inside my dev tooling, and the most
anchor-resistant model is again a Claude — the harness is a couple of
hundred lines of bash in the repo, and I'd still genuinely like to see
someone replicate it.

## The moral

We named the bias in the prompt and asked the models to set it aside.
They drifted anyway — less, but they drifted — and never once mentioned
the vote they'd been warned about. A bias that survives being named and
operates in silence doesn't get fixed by a warning label; it gets fixed
by making the anchor impossible to see. That's not a feature of
point.vote so much as the reason it exists. You can't ask your way to
independence. You have to build it.
