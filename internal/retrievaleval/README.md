# retrievaleval — does retrieval find the file the fix touches?

Measures **recall@k** for repository retrieval on SWE-bench instances: for each
instance it checks out the repo at its base commit, indexes it, retrieves against
the problem statement, and asks whether the **gold patch's changed files** appear
among the top-k retrieved chunks. No model runs, so this is **$0** — it is the
free tuning signal for chunk size, `k`, and fusion before any generation spend.
Full design: [`docs/retrieval-design.md`](../../docs/retrieval-design.md).

## Why it exists

Retrieval quality and generation quality fail differently. If the agent never
sees the file the fix lives in, no amount of prompting saves it. recall@k isolates
that first half and lets you get it right for nothing — measured directly against
gold patches, the same discipline as the SWE-bench gold validation.

## What's here

| Piece | File | Notes |
|---|---|---|
| `Task`, `Config`, `EvaluateTask`, `Run` — chunk → index → retrieve → recall | `eval.go` | pure; one chunk pass + one index build per mode, all `k` by truncation |
| `ModeBuilder` — `lexical \| semantic \| hybrid` → an index builder | `eval.go` | lexical needs no embedder; semantic/hybrid need one |
| `Aggregate`, `Summarize`, `FormatTable` — per-`(mode,k)` mean recall + full-hit rate | `report.go` | the comparison table |
| `RepoSource` seam + `GitRepoSource` (validated, shallow, single-commit fetch) | `source.go` | read-only; never executes repo code |

The metric itself (`ChangedFiles`, `RecallAtK`) lives in
[`internal/index`](../index); this package wires it to the dataset behind a
checkout seam.

## Safety properties

- **Read-only.** Indexing only reads files; the repo is never built or run. The
  chunker skips binaries, symlinks, oversized files and dependency dirs, and is
  chunk-capped.
- **Validated at the boundary.** `repo` (`owner/name`) and `base_commit` (hex SHA)
  are checked against strict allowlists before reaching `git`, so neither can
  smuggle a flag or a different URL. git runs non-interactively, isolated from
  ambient config and credential helpers, under a per-command timeout.
- **Run it in the sandbox.** It fetches arbitrary public repositories — do that on
  CI/ephemeral runners, not a personal machine.
- **Offline-testable.** A fixture `RepoSource` and the deterministic mock embedder
  exercise the whole flow with no network; one test drives the real `git` path
  against a local repo over the file transport (no network), and the live dataset
  run happens on Actions.

## Try it

```bash
# lexical — no embedder, no key, $0:
go run ./cmd/swebench-recall -instances slice.jsonl -modes lexical -k 5,10,20

# hybrid — point EMBED_BASE_URL at a local Ollama for free embeddings:
EMBED_BASE_URL=http://localhost:11434/v1 EMBED_MODEL=nomic-embed-text \
  go run ./cmd/swebench-recall -instances slice.jsonl -modes lexical,hybrid -k 10
```

`slice.jsonl` is a SWE-bench dataset export (JSONL or JSON array). On CI,
[`.github/workflows/swebench-recall.yml`](../../.github/workflows/swebench-recall.yml)
selects a Lite slice from the live dataset and runs lexical recall for $0.
