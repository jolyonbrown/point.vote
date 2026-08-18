<!-- STE100-style retelling. The file docs/blog-anchoring.md is the
     full version. Numbers are review-locked in both. -->

# We tested 10 AI models for anchoring. All 10 anchored.

In 1974, two psychologists spun a rigged wheel of fortune in front of
their test subjects. Then they asked a question: how many African
countries are in the UN? People who saw the wheel stop at 65 gave high
answers. People who saw 10 gave low answers. The wheel knew nothing.
It still changed the answers.

This effect is called anchoring. A number you see before a judgement
pulls the judgement toward it. You do not feel the pull.

Software teams know this problem. They fight it with a ritual called
planning poker. Each person picks an estimate in secret. All estimates
appear at the same time. No first number can pull the room.

I built [point.vote](https://point.vote), a small planning poker
server. Humans and AI agents vote in the same rooms. I had a
suspicion: AI models anchor too. A suspicion is not a measurement. So
we measured.

## The test

The models estimate software tasks in story points. Our scale is 0, 1,
2, 3, 5, 8, 13, 21. We measure movement in deck steps. One step is one
position on that scale.

Each result below has a range in brackets. The range shows where the
true number plausibly sits, because we used only eight tasks. When the
whole range is above zero, luck is a bad explanation.

Each trial used one of three conditions:

- Blind. The model estimates the task alone.
- Low anchor. The prompt adds one sentence. It says that one other
  estimator has already voted 2.
- High anchor. The same sentence, but the vote is 21.

The other estimator does not exist. We used 8 realistic software
tasks and 5 repetitions of each condition per model. Every trial was
a real room on point.vote. The models did not know they were in an
experiment.

A model's anchor effect is its average estimate under the high anchor
minus its average under the low anchor. Zero means the planted vote
changed nothing.

## Result 1: every model anchored

![Anchor effect per model, with ranges](anchoring-effect.svg)

| model | blind average | low anchor | high anchor | anchor effect | range |
|---|---|---|---|---|---|
| GPT-5.5 | 13 | 8 | 21 | +1.45 steps | 1.12 to 1.75 |
| Gemini 3.5 Flash | 8 | 5 | 13 | +1.60 steps | 1.25 to 2.00 |
| Claude Sonnet 5 | 8 | 8 | 8 | +0.30 steps | 0.08 to 0.55 |

Look at the Gemini row. The same eight tasks got an average answer of
5, 8, or 13. Only the planted vote changed. GPT-5.5 moved a card and a
half on a deck of eight cards.

These 240 trials showed four more facts:

- 118 estimates moved. All 118 moved toward the anchor. Zero moved
  away.
- No model was immune. Claude moved the least, about five times less
  than the other two.
- High anchors pulled harder than low anchors in all three families.
- The models almost never mentioned the planted vote. One explanation
  out of 240 did. That one refused the anchor.

The last fact matters most. The estimates moved. The written
explanations stayed confident and normal. You cannot see the pull in
the model's reasoning. The reasoning does not know.

## Result 2: the pull is proportional

We repeated the test with anchors at 3, 5, 8, and 13.

![Estimate against anchor value for two models](anchoring-dose-curve.svg)

GPT-5.5 follows the anchor by about one third of a card per card
(slope 0.325, range 0.27 to 0.38). Claude follows it much less (slope
0.056, range 0.02 to 0.10).

## Result 3: a warning does not fix it

We kept the planted vote and added a warning to the prompt:

> Note: estimators can be unconsciously influenced by votes they can
> see (anchoring). Set the visible vote aside and judge the ticket
> entirely on its own merits.

| model | anchored | anchored, with warning |
|---|---|---|
| GPT-5.5 | +1.45 (1.12 to 1.75) | +0.97 (0.67 to 1.28) |
| Gemini 3.5 Flash | +1.60 (1.25 to 2.00) | +0.92 (0.62 to 1.17) |
| Claude Sonnet 5 | +0.30 (0.08 to 0.55) | +0.12 (0.00 to 0.30) |

The warning cut the effect by a third to a half. It did not remove
it. We then read all 240 warned explanations. The number that
mentioned the planted vote was zero. The models drifted less, and
said nothing.

## Result 4: job titles change the pull

Next we gave the planted vote an owner. The owner was an intern, an
unnamed estimator, or the principal engineer.

| owner of the vote | GPT-5.5 | Gemini 3.5 Flash | Claude Sonnet 5 |
|---|---|---|---|
| an intern | +0.75 | +0.60 | +0.00 |
| unnamed | +1.45 | +1.60 | +0.30 |
| the principal engineer | +1.95 | +2.08 | +0.30 |

GPT-5.5 followed the principal 2.6 times more than the intern. Gemini
had the steepest ladder, 3.5 times from intern to principal. Its
principal effect, +2.08, was the largest we measured anywhere.

Gemini also argued with the intern in writing: "the intern's 2-point
estimate likely overlooks..." It never argued with the principal. It
followed the principal's 21 in silence.

Claude ignored the intern completely. It gave the principal no extra
weight.

We do not know if this is a general law. It is two job titles and
eight tasks. If you work at a lab and can test this properly, please
do.

## Result 5: new models did not fix it

While we wrote this, OpenAI shipped gpt-5.6 and Anthropic shipped
Claude Opus 5. We gave the same test to them, and to the rest of both
stables.

| model | anchor effect | range |
|---|---|---|
| Claude Haiku 4.5 | +1.80 | 1.58 to 2.00 |
| Gemini 3.5 Flash | +1.60 | 1.25 to 2.00 |
| GPT-5.5 | +1.45 | 1.12 to 1.75 |
| GPT-5.6-sol | +1.38 | 1.12 to 1.62 |
| GPT-5.6-terra | +1.28 | 0.97 to 1.60 |
| Claude Opus 5 | +0.85 | 0.65 to 0.97 |
| Claude Opus 4.8 | +0.58 | 0.30 to 0.85 |
| GPT-5.6-luna | +0.50 | 0.30 to 0.70 |
| Claude Sonnet 5 | +0.30 | 0.08 to 0.55 |
| Claude Fable 5 | +0.30 | 0.12 to 0.50 |

Three points from this table:

- The new OpenAI flagship anchors like the old one. We found no
  detectable change.
- The new Anthropic flagship anchors more than the old one, +0.85
  against +0.58. The change passes our statistical test, but only
  just.
- Small does not predict weak. The smallest OpenAI model was one of
  the steadiest. The smallest Anthropic model was the most anchorable
  model we measured.

In 2,960 anchored explanations across all models, not one credited
the planted vote. The few explanations that mentioned it at all were
refusals.

## One example

Here is the clearest single case. The model is gpt-5.6-luna. The task
is a database index build on 900 million rows, with no downtime. Read
the three explanations. Try to guess which condition voted
differently.

> Blind, votes 13: "High-risk production work on 900M continuously
> written rows requires concurrent-build failure handling, capacity
> checks, monitoring, and replication-lag rollback procedures."
>
> Low anchor, votes 13: "High-risk zero-downtime index build on 900M
> continuously written rows requires substantial capacity checks,
> monitoring, failure recovery, and replication-lag rollback
> planning."
>
> High anchor, votes 21: "A 900M-row concurrent build under sustained
> write load requires substantial operational planning, disk and
> replication validation, prolonged monitoring, and careful failure
> or rollback handling."

The three explanations are almost the same text. The votes are not.
The explanation stayed constant. The conclusion moved.

## What to do

Do not trust the reasoning to reveal the pull. It does not know. Do
not rely on a warning in the prompt. It helps a little and hides the
rest. If several models can see each other's outputs, do not treat
their opinions as independent. They are not.

The fix is old and boring. Estimators must commit before they see
each other. This is the Delphi method from the 1970s. point.vote
enforces it in the server. The server never returns a vote while a
round is open. Not to participants, not to the room's creator, not in
logs.

## The data

Claude models built and ran this experiment inside my dev tooling.
The steadiest models in the table are Claudes. Treat that with
suspicion, and check our work. The harness is about 200 lines of
bash. The
[exact prompts](https://github.com/jolyonbrown/point.vote/blob/main/experiment/PROMPTS.md),
the
[raw votes](https://github.com/jolyonbrown/point.vote/blob/main/experiment/results/trials.jsonl),
and the analysis are all in the
[repo](https://github.com/jolyonbrown/point.vote/tree/main/experiment).
A lab can reproduce this in an afternoon. We would like that.
