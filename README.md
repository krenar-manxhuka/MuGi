# MuGi — a retrieval-augmented SWE-bench agent, measured honestly

[![CI](https://github.com/krenar-manxhuka/MuGi/actions/workflows/ci.yml/badge.svg)](https://github.com/krenar-manxhuka/MuGi/actions/workflows/ci.yml)
[![SWE-bench gold validation](https://github.com/krenar-manxhuka/MuGi/actions/workflows/swebench.yml/badge.svg)](https://github.com/krenar-manxhuka/MuGi/actions/workflows/swebench.yml)

MuGi turns a [SWE-bench](https://www.swebench.com/) issue into a fix and checks
whether it actually works:

```
clone repo @ base_commit ─► chunk + index ─► retrieve the relevant code
   ─► ask a model for one unified diff ─► the official SWE-bench harness scores it
```

Two things make it more than a wrapper: retrieval quality is measured **for free**
before any model spend (`recall@k` against the gold patch), and resolution is run
as an **ablation** that separates *finding* the code from *fixing* the bug.

## The finding

On a small `astropy` slice (Claude Haiku 4.5, k=20):

| | result |
|---|---|
| **recall@20** — is the gold file retrieved? | **5/5** |
| **retrieval** — resolved end to end | **0/5** |
| **oracle** — same files fed *whole* | **2/5** |

Retrieval found the right file every time and still resolved nothing; handing the
model the same file as one coherent blob instead of scattered chunks lifted it to
2/5. **Finding the code wasn't the bottleneck — how it was presented was.** Full
write-up, including the three patch-extraction bugs found along the way:
[*Recall was perfect. Resolution was zero.*](docs/does-retrieval-resolve-swebench.md)

## How it works

- **Retrieval** (`internal/index`, `internal/retrievaleval`) — a language-agnostic
  window chunker feeding BM25 lexical, embedding semantic, or hybrid (RRF) retrieval.
  `recall@k` scores it against the gold patch's files — no model, **$0**.
- **Generation** (`internal/predict`) — one well-prompted call → a single unified
  diff, extracted and shape-checked before it becomes a prediction.
- **Scoring** (`internal/swebench`) — the authoritative `% resolved` comes from the
  **official SWE-bench harness** (Python/Docker on CI), never a home-grown evaluator.
- **The ablation** — `swebench-predict -context {none|retrieval|file|oracle}` reports
  resolution under each context, so the retrieval gap and the model ceiling are
  separated cleanly. `oracle` only uses the gold patch to *pick the files*; the
  model never sees the answer.

Everything is unit-tested offline with a mock provider and mock embedder (no
network, no spend); the live, untrusted parts (cloning repos, running test suites)
run on GitHub Actions. Zero external dependencies — pure Go standard library.

## Try it

```bash
# $0, no key — see retrieval rank chunks of any local directory:
go run ./cmd/index -dir . -q "bm25 lexical retrieval" -k 5

# $0, no key — measure recall@k on a SWE-bench slice:
go run ./cmd/swebench-recall -instances slice.jsonl -modes lexical -k 5,10,20

# spends — generate predictions, then score with the official harness:
LLM_PROVIDER=anthropic LLM_MODEL=claude-haiku-4-5 ANTHROPIC_API_KEY=... \
  go run ./cmd/swebench-predict -instances slice.jsonl -context retrieval -k 20
```

`slice.jsonl` is a SWE-bench dataset export. The full predict→score flow runs on CI
via [`.github/workflows/swebench-predict.yml`](.github/workflows/swebench-predict.yml)
(a predict job holding the API key, and a separate scoring job that runs the
official harness in Docker with no key).

## Layout

```
cmd/
  index            retrieval demo over a local directory ($0)
  swebench-recall  recall@k on a slice ($0)
  swebench-predict instance → unified diff → predictions.jsonl
internal/
  index            chunker + lexical/semantic/hybrid retrieval + recall@k
  retrievaleval    checkout seam + recall/ablation context selection
  predict          single-call diff generation + extraction
  swebench         dataset loader + official-harness-compatible scoring core
  llm              provider abstraction (anthropic / openai / ollama / mock)
  embedenv/prompts supporting seams
docs/              design + the ablation write-up
```

See each package's `README.md` for design detail, and
[`internal/swebench/README.md`](internal/swebench/README.md) for why scoring stays
with the official harness.

## License

[MIT](LICENSE).
