# index — repository retrieval

Builds a retrieval index over a repository so an agent can be shown only the code
relevant to a task, instead of a whole repo that won't fit in a context window.
This is the prerequisite for MuGi generating real SWE-bench patches. Full design:
[`docs/retrieval-design.md`](../../docs/retrieval-design.md).

## What's here

| Piece | File | Notes |
|---|---|---|
| Language-agnostic line-window chunker (skips binaries, symlinks, oversized files, dependency dirs; chunk-capped) | `chunk.go` | read-only; never executes the repo |
| **Lexical** BM25 retrieval with an identifier-aware tokenizer (`FizzBuzz`→`fizz`,`buzz`) | `lexical.go` | strong free baseline |
| **Semantic** retrieval by embedding cosine similarity | `semantic.go` | M2 |
| **Hybrid** lexical⊕semantic via Reciprocal Rank Fusion | `hybrid.go` | M2 — robust, no score calibration |
| `Embedder` seam + deterministic `MockEmbedder` + real OpenAI-compatible `OpenAIEmbedder` | `embedder.go`, `openai_embedder.go` | works with OpenAI, Voyage, or local **Ollama** (free) |
| Content-addressed embedding **cache** (in-memory + JSON file) so reruns don't re-pay | `cache.go` | M2 |
| `recall@k` against a gold patch — measure retrieval **for free**, no model generation | `recall.go` | the tuning signal |

Everything is unit-tested offline with **no network and no spend** — the real
embedder is tested against an `httptest` server, the rest with the mock embedder.

## Try it

```bash
# lexical — offline, $0:
go run ./cmd/index -dir . -q "bm25 lexical retrieval" -k 5

# hybrid — point at a local Ollama embedder for $0, or a hosted one with a key:
EMBED_BASE_URL=http://localhost:11434/v1 EMBED_MODEL=nomic-embed-text \
  go run ./cmd/index -mode hybrid -q "graceful shutdown" -k 5
```

## Measuring recall on real instances

[`internal/retrievaleval`](../retrievaleval) wires the `recall@k` metric here to
SWE-bench instances: it checks out each repo at its base commit, indexes it, and
scores whether retrieval surfaces the files the gold patch changed — the free
tuning signal. Run it via `cmd/swebench-recall` (lexical is $0).

## Next (M3)

Wire retrieval into the agent → `predictions.jsonl` → the official SWE-bench
harness, with the `{no-context, hybrid, oracle}` ablation. The discipline holds:
tune retrieval with **`recall@k` against gold patches** (free, or cents of local
embeddings) before spending anything on generation.
