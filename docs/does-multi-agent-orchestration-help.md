# Does multi-agent orchestration beat a single LLM call? I measured it.

Short version: **for a capable model, no — a single well-prompted call solved
*more* tasks than a four-agent pipeline, at a quarter of the cost.** And along the
way the pipeline's "reviewer" agent approved broken code nine times.

I kept hearing that wrapping an LLM in a pipeline of specialized agents —
planner, coder, reviewer — makes it write better code. It sounds right. So I
built a harness to check whether it's actually true, instead of assuming it.

This is what I found, and how the harness is built so you don't have to take my
word for it.

## The setup

[MuGi](https://github.com/krenar-manxhuka/MuGi) is an execution-grounded
benchmark for code-generation agents. Every artifact a model produces is
**compiled and tested** — `go build` and `go test` — and scored on the result,
not on the model's opinion of its own work. It runs 12 small Go tasks (4 easy,
5 medium, 3 hard), each with an exact spec: module name, function signatures, and
required behaviour.

To answer "does orchestration help?", it runs each task two ways:

- **`pipeline`** — the full loop: Coordinator → Planner → Coder → Reviewer, with
  up to 3 revision cycles.
- **`single`** — one Coder call, straight from the task, no plan and no review.

Same model, same tasks, same objective scoring. The only thing that changes is
the orchestration. I ran both strategies on Claude Haiku 4.5 and Claude Sonnet
4.6 — 48 runs, every artifact built and tested. Total cost: **$2.66.**

## Finding 1: the LLM reviewer rubber-stamps code that fails its own tests

The pipeline has a Reviewer agent whose job is to catch bad code. It is handed the
real `go build` / `go test` output before it decides. And across the two pipeline
runs it **approved failing artifacts nine times** (Haiku 5, Sonnet 4) — scoring
them ~9/10 *while their tests were red*.

The harness only caught this because it computes pass/fail from execution itself,
independently of the reviewer, and an **objective gate** overrides any "approval"
whose build or tests are red. Without that gate, every one of those nine would
have shipped as "approved, 9/10."

The takeaway isn't "this reviewer is bad." It's structural: **you cannot delegate
the quality gate to an LLM reviewer, even when you hand it the failing test
output.** Reviewing one nice-looking program by hand reproduces exactly this blind
spot — the model's holistic "this looks like correct Go" sense overrides the
explicit failing signal. Only scoring execution across every artifact makes it
visible.

## Finding 2: orchestration helps a weak model and hurts a strong one

Here's the run, every artifact built and tested:

| Model | Strategy | Tests pass | False approvals | Cost | $/solved |
|---|---|---:|---:|---:|---:|
| Haiku 4.5 | pipeline | 11/12 | 5 | $0.64 | $0.058 |
| Haiku 4.5 | single | 7/12 | 0 | $0.22 | $0.031 |
| Sonnet 4.6 | pipeline | 10/12 | 4 | $1.43 | $0.143 |
| Sonnet 4.6 | **single** | **11/12** | 0 | **$0.38** | **$0.034** |

Two opposite stories:

- **Haiku (the weaker model): the pipeline helped** — 11/12 vs 7/12 — but cost
  ~3× as much. The plan-and-revise loop compensates for a less capable model.
- **Sonnet (the stronger model): the single call won** — 11/12 vs 10/12 — at
  **~¼ the cost** and ⅕ the latency. Being charitable, one of the pipeline's two
  misses was an API error rather than the model, so call it a tie at best — for
  4× the money.

The most pointed example: the **Pub/Sub** task. A single Sonnet call got it right.
The pipeline took that same model, ran it through plan-and-review, and **produced
a *broken* solution that its reviewer approved three times** before the objective
gate forced it to keep trying. The orchestration didn't just fail to help — it
turned a correct answer into a wrong one.

The number that actually compares engineering choices is **cost per solved task.**
For Sonnet that's $0.034 single vs $0.143 pipeline. The orchestration is dominated.

**My read:** multi-agent orchestration buys capability for a weak model and little
or nothing for a strong one — always at 3–4× the cost. If your model is good, a
single well-prompted call is the better engineering choice on tasks this size.
"More agents" is not a free improvement; it's a cost you should be able to justify
with a number.

## Finding 3: the wins are real, not self-graded

There's a subtle trap in "tests pass": the coder writes both the code *and* the
tests, so it can pass by writing weak tests. To guard against that, five of the
tasks carry a **hidden acceptance test** — written by the harness, never shown to
the model, run separately against the implementation.

On those five tasks, every passing solution also passed its hidden test (5/5
across all four configs). So at least on the covered tasks, the self-reported
passes aren't gamed. The next improvement is extending hidden coverage to the hard
tasks, where the failures cluster.

## Where I'm being careful (the honest caveats)

A finding is only worth as much as its limitations are stated:

- **One run, no repeated trials.** Model output varies; a single pass rate
  conflates capability with luck. `pass@k` is the obvious next step.
- **12 small greenfield Go tasks.** These aren't real repositories. The harness
  also integrates SWE-bench Lite for that, but the headline numbers here are toy
  tasks — they bound the claim.
- **One pipeline shape.** I tested *this* Coordinator/Planner/Coder/Reviewer
  design. A different multi-agent topology might do better; what I can say is that
  this common one didn't beat a single call on a strong model.
- **One of Sonnet's pipeline failures was infrastructure** (an API `EOF`), not the
  model. The harness records that as a distinct error row, never as a model
  failure — so it doesn't get miscounted as "Sonnet can't solve worker pools."

## Reproduce it

Everything is reproducible from the repo. The mock provider runs the whole harness
offline with no API key:

```bash
go run ./cmd/bench -providers mock                  # offline self-test
go run ./cmd/bench -providers sonnet,haiku -strategy both   # the run above
```

Each row streams to `results.csv` as it finishes, and every run writes a
`run.json` provenance stamp (git SHA, model IDs, Go version). The exact numbers
above, with captured failure output for every miss, are in
[`bench/results/RESULTS.md`](../bench/results/RESULTS.md).

## The point

The interesting artifact here isn't the agent. It's the harness that can tell you
— honestly, with execution behind every number — whether the agent is worth it.
For this pipeline, on these tasks, the answer for a capable model was: it isn't.
I'd rather know that than ship a pipeline that *feels* sophisticated and quietly
costs 4× for no gain.
