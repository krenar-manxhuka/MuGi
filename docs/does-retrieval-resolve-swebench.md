# Recall was perfect. Resolution was zero. A SWE-bench retrieval ablation.

Short version: on a small SWE-bench Lite slice, retrieval found the file the gold
patch touches **every single time** (recall@20 = 5/5) — and the end-to-end agent
still resolved **0 of 5**. Handing the model the *same* file as one coherent blob
instead of scattered top-k chunks lifted it to **2/5**. So the bottleneck wasn't
finding the right code; it was **how the code was presented to the generator**.
The remaining gap is the model itself.

This is a tiny slice (5 instances, one repo, one model, one run) — not a
leaderboard number. It's a method, and a finding I didn't expect.

## The setup

[MuGi](https://github.com/krenar-manxhuka/MuGi) turns a SWE-bench instance into a
prediction: clone the repo at its base commit, retrieve the relevant code, ask a
model for a single unified diff, and let the **official SWE-bench harness** apply
and score it. The harness is authoritative — it runs the real repo's test suite in
Docker and decides resolved/not-resolved. I don't grade my own homework.

The discipline that made this cheap: measure retrieval **before** spending on
generation. `recall@k` asks "are the gold patch's files among the top-k retrieved
chunks?" — no model, $0. Only once recall looked good did I spend a cent on the
agent. The whole study cost about **$0.40** in API calls (Claude Haiku 4.5).

To separate *finding* the code from *fixing* the bug, the agent runs under four
context conditions — same model, same instances, only the context changes:

| condition | what the model sees |
|---|---|
| `none` | nothing but the issue (lower bound) |
| `retrieval` | the top-k retrieved chunks (the real system) |
| `file` | every chunk of the top-ranked file (retrieval picks the file, model sees it whole) |
| `oracle` | exactly the gold patch's files (upper bound — isolates the model) |

`oracle` only uses the gold patch to *choose the files*; the model is never shown
the answer. It's the ceiling: the best retrieval could ever do is hand over the
right file.

## The free signal: recall is not the problem

Lexical BM25 retrieval, no embeddings, $0:

| k | recall |
|---|---|
| 5 | 0.80 |
| 10 | 0.80 |
| 20 | **1.00** |

At k=20 the gold file is in the retrieved set for all 5 instances. If resolution
were bottlenecked on *finding* the file, we'd be in good shape.

## Before any of that worked, the patches had to actually apply

The first runs resolved almost nothing — not because the fixes were wrong, but
because the model's diffs wouldn't apply. Three distinct, deterministic bugs, each
found by reading the harness's own apply logs and fixed against the real failing
patches:

1. **Trailing junk.** The model emitted a diff, then a `</diff>` tag, then
   "wait, let me reconsider…", then a *second* diff. The extractor took everything
   to end-of-output → `malformed patch ... </diff>`. Fix: keep only the first
   well-formed diff, stop at the first line that can't belong to it.
2. **Bare blank lines.** Context blank lines came out empty instead of as a single
   space, which `patch` rejects. Fix: normalize them.
3. **Miscounted hunks.** Headers like `@@ -237,10 +237,14 @@` for a hunk that
   actually spans 12/15 lines → `patch` rejects a stray mid-hunk line. Fix:
   recompute the counts from the body.

After these, **100% of patches applied** under the oracle condition (0 harness
errors). The format problem was solved; what remained was real.

## The result

Per condition, on the same 5 instances (Haiku 4.5, k=20):

| condition | resolved |
|---|---|
| retrieval | **0 / 5** |
| oracle | **2 / 5** |

Retrieval finds the file 5/5 of the time and resolves 0. Oracle — the same files,
fed whole — resolves 2. The gap is entirely about presentation.

### The case that explains it: `astropy-12907`

- **Oracle**: handed `separable.py` whole, the model edited the right function
  (`_cstack`), its context lines matched, the patch applied, the tests passed.
  **Resolved.**
- **Retrieval**: handed the top-k chunks of the same file (plus distractor chunks
  from other files), the model edited a *different* plausible function
  (`_separable`) and wrote context that didn't match the real file. `Hunk #1
  FAILED`. **Errored.**

Same model, same bug, same temperature. The only difference was whether it saw the
file as a coherent whole or as fragments. Fragments cost it both the localization
and the context accuracy.

## What it means, and what I changed

Two clean, separated levers:

1. **Presentation.** Scattered top-k chunks are worse context than the whole file,
   even when recall is perfect. So I added a `file` context mode: retrieval ranks
   the files, then the model is shown the top file(s) *whole* — oracle-shaped
   context, but chosen from the problem statement, not the answer. This targets the
   0→2 gap directly. (Implemented and unit-tested; evaluating it is the next run.)
2. **The model.** Oracle's 2/5 is a ceiling: three of these astropy bugs Haiku
   can't fix even when handed the exact file. Lifting that needs a stronger model,
   not better retrieval — and notably *not* a fancier patch format, since with the
   fixes above every patch already applies.

## Honest limits

Five instances, all from `astropy` (one of the harder SWE-bench Lite repos), one
small model, a single run. These numbers don't generalize and aren't meant to —
the point is the *method*: measure recall for free, separate retrieval from
generation with an oracle baseline, and let the official harness decide. That rig
is what turned "the agent resolved nothing" from a dead end into a specific,
actionable diagnosis.

## Reproduce it

```bash
# free: does retrieval even find the file?
go run ./cmd/swebench-recall -instances slice.jsonl -modes lexical -k 5,10,20

# the ablation (spends; needs an Anthropic key). Vary -context across the four:
LLM_PROVIDER=anthropic LLM_MODEL=claude-haiku-4-5 ANTHROPIC_API_KEY=... \
  go run ./cmd/swebench-predict -instances slice.jsonl -context retrieval -k 20
```

The same flow runs on CI via `.github/workflows/swebench-predict.yml` (a predict
job that holds the key and never runs untrusted tests, and a separate scoring job
that runs the official harness in Docker with no key).
