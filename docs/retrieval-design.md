# Design: repository retrieval for SWE-bench

**Status:** proposed · **Owner:** —  · **Prereq:** SWE-bench harness (done, gold-validated)

## 1. Goal & success criteria

Give MuGi's agent the *relevant* code from a real repository so it can produce a
correct patch — and **prove the retrieval helps** rather than assume it.

Success is measurable, in two layers:

1. **Retrieval quality (free):** `recall@k` ≥ a target on a SWE-bench Lite slice —
   i.e. the top-k retrieved chunks contain the files the *gold patch* actually
   changed. Measured directly against the dataset, **no model generation, $0**.
2. **End-to-end lift (costs generation):** SWE-bench resolution rate rises from
   *no-context* → *retrieval*, and approaches the *oracle* upper bound.

### Non-goals (first cut)
- Not a general code-search product; just enough to localize a fix.
- No per-language AST parsing yet — start language-agnostic (SWE-bench repos are
  Python; MuGi is Go).
- Don't touch scoring — the official SWE-bench harness stays authoritative.

## 2. Architecture (reuses existing seams)

A new pure package `internal/index`, mirroring the `llm.Provider` philosophy so it
is fully testable offline:

```go
type Chunk struct { Path string; Start, End int; Content string; Symbols []string }

// Chunker turns a checked-out repo into retrievable units.
type Chunker interface { Chunk(root string) ([]Chunk, error) }

// Embedder mirrors llm.Provider: real impls hit an API; the mock is deterministic
// so the whole pipeline unit-tests with no network.
type Embedder interface { Embed(ctx, texts []string) ([][]float32, error); Name() string }

// Index holds chunks + a lexical index + (optionally) embeddings.
type Index interface { Retrieve(ctx, query string, k int) ([]Chunk, error) }
```

- **Hybrid retrieval** = lexical (BM25) ⊕ semantic (cosine over embeddings),
  combined by reciprocal-rank fusion. Lexical alone is a strong, free baseline;
  semantic catches relevance the keywords miss.
- **Caching:** embeddings keyed by `sha256(chunk)` so re-indexing is cheap and runs
  are reproducible; the embedder model id goes into `run.json` provenance.

## 3. Pipeline

```
clone @ base_commit ─► walk (skip binaries/vendored/oversized; honor .gitignore)
   ─► Chunk ─► Embed (batched + cached) ─► build lexical index ─► Index
problem_statement ─► Index.Retrieve(k, token-budget) ─► top chunks ─► agent prompt
   ─► agent emits a unified diff ─► predictions.jsonl ─► official harness scores it
```

## 4. Measurement (non-negotiable — this is MuGi's identity)

| Layer | What | Cost |
|---|---|---|
| **recall@k** | Do retrieved chunks include the gold patch's changed files? | **$0** (no generation) |
| **end-to-end ablation** | Resolution under `{no-context, lexical, semantic, hybrid, oracle}` | generation $ |

- **`recall@k` lets you get retrieval *right* for free** — tune chunk size, `k`,
  and fusion weights against the dataset before spending on the model.
- **Oracle baseline** = feed exactly the files the gold patch touches. It is the
  upper bound that **isolates retrieval quality from generation quality**: if
  `hybrid ≈ oracle`, retrieval is solved; the gap *is* the retrieval headroom.

## 5. Best practices

- **Interface-driven + a mock embedder** → the entire index/retrieve pipeline is
  unit-testable offline with no network (same discipline as the mock provider and
  the SWE-bench `LocalEnv`).
- **Deterministic & cached** → content-hash keyed embeddings; record the embedder
  model in provenance; reproducible runs.
- Pure, small functions; table tests for chunking and ranking; a tiny local-repo
  fixture proves end-to-end retrieval offline (like `eval_test.go`).
- Config via env/flags: `k`, token budget, fusion weights, embedder.

## 6. Safety

- **No code execution.** Indexing is read-only text processing — unlike build/test,
  it never runs the repo. Parsing untrusted repo *text* is safe.
- **Data egress:** embedding sends repo code to the embeddings API. SWE-bench repos
  are public, so this is acceptable; **documented** as a constraint (a private-repo
  user would need a local embedder). Skip obvious secret files (`.env`, keys) even
  though public repos shouldn't contain them.
- **Untrusted repos** are cloned/indexed inside the **same sandbox boundary** as the
  harness (Docker / Actions), never on a dev machine. Resource caps: max files, max
  chunks, max bytes/chunk, timeouts.
- **Keys:** the embedding API key is handled like the model key — kept in a job that
  does **not** run untrusted repo tests (the job-separation rule already noted for
  the agent step).

## 7. Phased milestones

| # | Deliverable | Spend |
|---|---|---|
| **M1** | `internal/index`: chunker + **lexical** retrieval + **mock embedder** + `Retrieve`, fully offline-tested. Plus a **`recall@k` harness** against gold patches on a Lite slice. | **$0** |
| **M2** | Real `Embedder` (Anthropic/OpenAI embeddings) behind the interface + **hybrid** retrieval + caching. Tune `recall@k`. | ~cents (embeddings) |
| **M3** | Wire retrieval into the agent → `predictions.jsonl` → official harness → **first real MuGi SWE-bench number** (haiku, 1–3 instances) + the `{no-context, hybrid, oracle}` ablation. | generation $ (start small) |
| **M4** *(optional)* | AST-aware chunking, repo-map summaries, larger slice. | — |

**M1 alone is a complete, valuable, $0 deliverable**: "retrieval recall@k on real
SWE-bench instances," provably correct, no model spend — exactly the pattern that
worked for the gold validation.

## 8. Risks & open decisions

- **Embedder choice** — Anthropic vs OpenAI embeddings vs a local model. Decide at M2.
- **Chunk granularity** — function-level vs fixed-window. *Don't guess — pick via
  `recall@k`.*
- **`k` / token budget** — precision vs recall vs cost. *Measure.*
- **Cross-language** — language-agnostic chunking first; AST-aware is M4.
- **Biggest risk:** retrieval is good but generation still fails (the model can't
  fix the bug even when shown the code). The oracle baseline tells you which half
  to blame, so you never chase the wrong problem.
