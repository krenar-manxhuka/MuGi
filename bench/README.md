# MuGi bench

End-to-end evaluation harness: run the full MuGi pipeline against a suite of
small, well-specified Go tasks across one or more LLM providers, then capture
`go build` / `go test` outcomes, reviewer scores, revision counts, wall-clock
latency, token usage, and dollar cost into CSV + Markdown.

## Layout

```
bench/
├── tasks/         # one YAML per task; description IS the prompt fed to MuGi
├── results/       # written by `go run ./cmd/bench`
│   ├── results.csv
│   ├── summary.json    # full per-row data, including build/test output on failure
│   └── RESULTS.md      # rendered tables for the README
└── README.md      # this file
```

## Tasks

12 tasks across 3 tiers, all Go (so `go build` + `go test` is a meaningful
correctness signal):

| Tier | Tasks |
|---|---|
| easy | FizzBuzz, Unicode-Safe String Reverse, Memoized Fibonacci, HTTP Health Endpoint |
| medium | Token-Bucket Rate Limiter, LRU Cache (generic), JSON Config Loader, CSV-to-JSON, In-Memory URL Shortener |
| hard | In-Memory Pub/Sub with per-subscriber backpressure, Worker Pool with graceful shutdown, Arithmetic Expression Evaluator |

Each task YAML names explicit function signatures and required test cases so
that "tests pass" means "the LLM-written tests, which exercise the required
contract, passed" — not just "the file compiled."

## Run

No API key needed for the mock provider — it's a deterministic baseline that
validates the harness end-to-end:

```bash
go run ./cmd/bench -providers mock
```

Real models (skipped silently if their key is missing):

```bash
# Anthropic — needs ANTHROPIC_API_KEY
go run ./cmd/bench -providers sonnet,haiku

# Local Ollama — uses LLM_MODEL from env, or "llama3" by default
go run ./cmd/bench -providers ollama

# All four (will skip whichever isn't configured)
go run ./cmd/bench -providers mock,sonnet,haiku,ollama
```

Useful flags:

| Flag | Default | Purpose |
|---|---|---|
| `-tasks` | `bench/tasks/*.yaml` | Glob pattern; restrict with e.g. `bench/tasks/easy-*.yaml` |
| `-providers` | `mock,sonnet,haiku` | Comma-separated provider names |
| `-out` | `bench/results` | Output directory |
| `-max-revisions` | `3` | Coder→reviewer cycle cap |
| `-strategy` | `pipeline` | `pipeline` \| `single` \| `both` — orchestration strategy (see Ablation) |
| `-quick` | `false` | Run one task per tier only (smoke) |
| `-v` | `false` | Stream orchestrator logs to stderr |

## Ablation: does the multi-agent pipeline beat one call?

`-strategy` chooses how each artifact is produced:

- `pipeline` — the full Coordinator→Planner→Coder→Reviewer loop with revisions.
- `single` — one Coder call straight from the task (no plan, no review, no
  revision loop), then the same `go build`/`go test` scoring.
- `both` — runs each (task, provider) under both, so the rollup shows them side
  by side. This is the honest control for whether the extra agents earn their
  cost: same provider, same tasks, same objective gate.

```bash
go run ./cmd/bench -providers sonnet -strategy both
```

The provider rollup then carries a **Strategy** column, and `single` rows have no
reviewer (so `reviewer_score` is -1 and `revisions_used` is 0) — the difference
in `test_ok`, `llm_calls`, `cost_usd`, and latency between the two rows is the
result.

## Held-out acceptance tests (kill self-grading)

`test_ok` runs the **model's own** tests — the coder writes both the
implementation and the tests, so a model can pass by writing weak tests. To get
an independent signal, a task may carry a `hidden_test`: a Go test body (no
`package` clause) that the coder never sees. After the run, the bench writes the
artifact's implementation **without its own test files**, injects the hidden test
into the implementation's package, and runs it. The result is `hidden_test_ok` —
the held-out acceptance signal.

```yaml
# in a task YAML
hidden_test: |
  import "testing"

  func TestHiddenReverse(t *testing.T) {
      if Reverse("héllo") != "olléh" {
          t.Fatal("rune reversal is wrong")
      }
  }
```

The headline comparison is **`test_ok` (self) vs `hidden_test_ok` (held-out)**:
where a model passes its own tests but fails the hidden one, its tests were too
weak to catch its own bug. Every shipped hidden test is itself validated in CI
against a known-correct reference solution (see
[`internal/runner/hidden_test.go`](../internal/runner/hidden_test.go)), so a buggy
hidden test can't silently mis-score a model. Not every task has one yet; tasks
without a `hidden_test` report `hidden_present=false` and show `—`.

## What's captured

Per (task, provider, strategy) row:

| Column | Source |
|---|---|
| `strategy` | `pipeline` or `single` — which orchestration produced the artifact |
| `build_ok` | `go build ./...` exit code on the final artifact |
| `tests_present` | At least one `*_test.go` file in the artifact |
| `test_ok` | `go test ./...` exit code on the model's *own* tests (only meaningful if `tests_present`) |
| `hidden_present` | Whether the task carried a held-out acceptance test |
| `hidden_build_ok` | The implementation (minus its own tests) compiled |
| `hidden_test_ok` | The held-out acceptance test passed against the implementation |
| `reviewer_score` | The reviewer agent's final 0–10 score (-1 if no review, e.g. `single`) |
| `approved` | Did the reviewer approve the final revision? |
| `false_approvals` | Reviewer approvals the objective gate overrode because build/test was red |
| `revisions_used` | Number of coder→reviewer cycles consumed (0 = approved first try; always 0 for `single`) |
| `duration_ms` | Wall-clock for the full run |
| `input_tokens` / `output_tokens` / `llm_calls` | Summed across every agent call |
| `cost_usd` | `tokens × per-MTok rate` from the `pricing` table at the top of `cmd/bench/main.go` |

On failure, `summary.json` also includes truncated `build_out` / `test_out`
so you can debug why a particular run regressed.

## Pricing

Per-MTok USD rates live as constants in `cmd/bench/main.go`. Update them when
Anthropic re-prices. Mock and local Ollama are billed at $0.

## Adding a task

1. Drop a new YAML into `bench/tasks/`. The id (kebab-case) becomes the row
   key in CSV/Markdown; the tier should be `easy`, `medium`, or `hard`.
2. Write the `description` as a precise spec: module name, exact function
   signatures, required test cases. Vagueness in the prompt becomes noise in
   the eval.
3. Re-run `go run ./cmd/bench`. New row appears automatically.

## Adding a provider

Add an entry to `availableProviders()` in `cmd/bench/main.go` returning any
`llm.Provider`. The `countingProvider` wrapper handles token tallying for
free.
