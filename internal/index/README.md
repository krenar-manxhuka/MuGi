# index — repository retrieval

Builds a retrieval index over a repository so an agent can be shown only the code
relevant to a task, instead of a whole repo that won't fit in a context window.
This is the prerequisite for MuGi generating real SWE-bench patches. Full design:
[`docs/retrieval-design.md`](../../docs/retrieval-design.md).

## What's here (M1 — done, $0, fully offline)

| Piece | File |
|---|---|
| Language-agnostic line-window chunker (skips binaries, symlinks, oversized files, dependency dirs; chunk-capped) | `chunk.go` |
| BM25 lexical retrieval with an **identifier-aware tokenizer** (`FizzBuzz` → `fizz`,`buzz`; `parse_error` → `parse`,`error`) | `lexical.go` |
| `Embedder` seam + deterministic `MockEmbedder` (offline-testable; real embeddings + hybrid are M2) | `embedder.go` |
| `recall@k` against a gold patch — measure retrieval **for free**, no model generation | `recall.go` |

Everything is unit-tested offline with no network. Try it on any repo:

```bash
go run ./cmd/index -dir . -q "bm25 lexical retrieval" -k 5
```

## Next (M2 / M3)

- **M2:** a real `Embedder` (embeddings API) behind the interface + hybrid
  (lexical ⊕ semantic) retrieval + embedding cache.
- **M3:** wire retrieval into the agent → `predictions.jsonl` → the official
  SWE-bench harness, with the `{no-context, hybrid, oracle}` ablation.

The key discipline: tune retrieval with **`recall@k` against gold patches ($0)**
before spending anything on generation.
