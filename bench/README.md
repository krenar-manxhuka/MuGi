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
| `-quick` | `false` | Run one task per tier only (smoke) |
| `-v` | `false` | Stream orchestrator logs to stderr |

## What's captured

Per (task, provider) row:

| Column | Source |
|---|---|
| `build_ok` | `go build ./...` exit code on the final artifact |
| `tests_present` | At least one `*_test.go` file in the artifact |
| `test_ok` | `go test ./...` exit code (only meaningful if `tests_present`) |
| `reviewer_score` | The reviewer agent's final 0–10 score (-1 if no review) |
| `approved` | Did the reviewer approve the final revision? |
| `revisions_used` | Number of coder→reviewer cycles consumed (0 = approved first try) |
| `duration_ms` | Wall-clock for the full pipeline call |
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
