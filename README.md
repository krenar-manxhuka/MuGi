# MuGi — Multi-Agent Software Builder

MuGi is a **Go-native, provider-agnostic multi-agent pipeline** that turns a
one-line task description into a structured plan, working code, and a
peer-reviewed result — all driven by four collaborating LLM agents. Every
generated artifact is compiled and tested before the reviewer sees it, so the
review is grounded in real execution output, not LLM speculation.

---

## Benchmark

12 small Go tasks (4 easy / 5 medium / 3 hard), each with explicit function
signatures and required test cases. The pipeline is scored on
`go build` + `go test` results, the reviewer's final score, revisions used,
wall-clock latency, and dollar cost.

| Provider | Build pass | Test pass | Avg score | Avg revisions | Avg latency | Total cost |
|---|---:|---:|---:|---:|---:|---:|
| `mock` (harness baseline) | 12/12 | 12/12 | 9.0 | 0.0 | 9.3s | $0.0000 |
| `ollama/qwen3:1.7b` | _run `go run ./cmd/bench -providers ollama` to populate_ | | | | | |
| `anthropic/claude-sonnet-4-6` | _set `ANTHROPIC_API_KEY` and re-run_ | | | | | |
| `anthropic/claude-haiku-4-5` | _set `ANTHROPIC_API_KEY` and re-run_ | | | | | |

> **Why the Anthropic rows are empty:** running the two paid Anthropic models
> across all 12 tasks would have incurred non-trivial API cost, so those rows
> are left for whoever runs the harness next with their own key. The local
> Ollama row is testable on commodity hardware and will be populated in a
> follow-up run; a partial smoke run (3 tasks, `qwen3:1.7b`) already lives in
> [`bench/results-ollama-quick/`](bench/results-ollama-quick/RESULTS.md) for
> reference.

Full per-task detail: [`bench/results/RESULTS.md`](bench/results/RESULTS.md) ·
raw data: [`bench/results/results.csv`](bench/results/results.csv) ·
how to run: [`bench/README.md`](bench/README.md).

### What we learned from this run

- **Mock is a contract test for the harness, not a benchmark.** The mock
  provider returns the same canned HTTP-server artifact for every task
  description, so its 12/12 pass rate measures whether the orchestrator,
  executor, and scorer agree end-to-end — not whether an LLM can solve
  FizzBuzz. Treat the mock row as the "all green" baseline that proves the
  rig is working.
- **The eval surfaced a real bug in `runner.detectLang` on the first run.**
  It was reading only the first file's `Lang` field, which for any Go module
  is `go.mod` with `lang: "text"`. The runner was silently skipping build
  and test execution for every real artifact. Fixed in
  [internal/runner/runner.go](internal/runner/runner.go). This is the kind
  of thing only an eval harness catches — manual smoke tests would have kept
  shipping false `build_ok: false` results forever.
- **The build/test round-trip dominates per-task latency.** Each mock run
  takes ~9s end-to-end while the mock LLM itself returns instantly. That's
  the `go build` + `go test` cycle inside the temp dir. Implication: for a
  fast model (Haiku, local 7B), the executor becomes the bottleneck — worth
  caching across same-content artifacts if we ever batch-evaluate at scale.

(More observations land here as the Anthropic and Ollama rows fill in.)

---

## How it works

```
User prompt
    │
    ▼
┌─────────────┐
│ Coordinator │  Initialises the workflow, narrates progress, produces the final summary
└──────┬──────┘
       │
       ▼
┌─────────────┐
│   Planner   │  Turns the task into a structured, dependency-ordered execution plan
└──────┬──────┘
       │
       ▼                   ┌──────────┐
┌─────────────┐  ──────►  │ Reviewer │  Inspects the artifact; returns structured feedback
│    Coder    │  ◄──────  └──────────┘
└─────────────┘
  (loop ≤ MAX_REVISIONS times)
       │
       ▼
┌─────────────┐
│ Coordinator │  Final summary
└─────────────┘
       │
       ▼
  output/ directory
  (artifact files written to disk)
```

The **orchestrator** is pure Go — it sequences agents, enforces the revision
cap, and manages the shared workflow state.  No agent knows about any other
agent; they read from and write to a shared `WorkflowState` via locked methods.

The **LLM layer** is a single `Provider` interface.  Every agent calls only
`provider.Generate(ctx, req)`.  Swapping from the mock to Anthropic to Ollama
requires zero code changes — only a single environment variable.

---

## Quickstart (one command, no API key required)

```bash
git clone <repo-url>
cd mugi
make run
```

Or without `make`:

```bash
go run ./cmd/mugi "Build a simple Go HTTP server with a /health endpoint"
```

The mock provider is the default.  It returns realistic, deterministic responses
so the full pipeline runs offline.

---

## Configuration

Copy `.env.example` to `.env` and fill in the values you need:

```bash
cp .env.example .env
```

| Variable | Default | Description |
|---|---|---|
| `LLM_PROVIDER` | `mock` | `mock` \| `anthropic` \| `openai` \| `ollama` |
| `LLM_MODEL` | provider default | Model name override |
| `MAX_REVISIONS` | `3` | Max coder→reviewer cycles |
| `OUTPUT_DIR` | `output` | Where artifact files are written |
| `PROMPTS_DIR` | `prompts` | Directory for prompt template overrides |

Export variables in your shell, or prefix the command:

```bash
LLM_PROVIDER=anthropic ANTHROPIC_API_KEY=sk-ant-... \
  go run ./cmd/mugi "Build a REST API in Go"
```

---

## Provider setup

### Mock (default)

No configuration required.  Use this for development and CI.

```bash
make run
# or
LLM_PROVIDER=mock go run ./cmd/mugi "Build a Go CLI tool"
```

### Anthropic

```bash
export ANTHROPIC_API_KEY=sk-ant-...
export LLM_PROVIDER=anthropic
export LLM_MODEL=claude-sonnet-4-6   # optional, this is the default

go run ./cmd/mugi "Build a Go REST API"
```

Available models: `claude-opus-4-7`, `claude-sonnet-4-6`, `claude-haiku-4-5-20251001`

### OpenAI

```bash
export OPENAI_API_KEY=sk-...
export LLM_PROVIDER=openai
export LLM_MODEL=gpt-4o              # optional, this is the default

go run ./cmd/mugi "Build a Go REST API"
```

### OpenAI-compatible (Groq, Together, Mistral, …)

```bash
export LLM_PROVIDER=openai
export OPENAI_API_KEY=gsk_...
export OPENAI_BASE_URL=https://api.groq.com/openai/v1
export LLM_MODEL=llama-3.3-70b-versatile

go run ./cmd/mugi "Build a Go REST API"
```

### Ollama (local open-source models)

```bash
# 1. Install Ollama: https://ollama.com
# 2. Pull a model
ollama pull llama3

# 3. Run MuGi against it
export LLM_PROVIDER=ollama
export LLM_MODEL=llama3              # optional, this is the default

go run ./cmd/mugi "Build a Go REST API"
```

---

## Customising prompts

The `prompts/` directory at the project root contains editable template files.
Edit any `.tmpl` file to change how an agent behaves.  Changes take effect on
the next run — no recompile needed.

```
prompts/
  coordinator.tmpl   ← narrates workflow progress
  planner.tmpl       ← produces the execution plan
  coder.tmpl         ← implements the plan
  reviewer.tmpl      ← evaluates the artifact
```

MuGi checks `PROMPTS_DIR` (default `prompts/`) first.  If a file is not found
there it falls back to the compiled-in defaults in `internal/prompts/templates/`.

---

## Running tests

```bash
make test            # all tests
make test-verbose    # with -v output
make test-coverage   # HTML coverage report at coverage.html
```

All tests use the mock provider — no API key required.

---

## Project structure

```
.
├── cmd/
│   ├── mugi/               CLI entrypoint
│   └── bench/              Evaluation harness (see bench/README.md)
├── bench/
│   ├── tasks/              YAML task definitions (one per benchmark task)
│   ├── results/            CSV + Markdown + JSON output (regenerated each run)
│   └── README.md           How to run the bench, add tasks, add providers
├── internal/
│   ├── agents/             Agent interface + four implementations
│   │   ├── agent.go        Agent interface & JSON extraction helper
│   │   ├── coordinator.go
│   │   ├── planner.go
│   │   ├── coder.go
│   │   └── reviewer.go
│   ├── config/             Environment-based configuration
│   ├── llm/                Provider interface & adapters
│   │   ├── provider.go     Provider interface (the plug-and-play contract)
│   │   ├── mock.go         Deterministic mock for tests and local dev
│   │   ├── anthropic.go    Anthropic Messages API adapter
│   │   ├── openai.go       OpenAI-compatible adapter (also Ollama, Groq, …)
│   │   └── registry.go     NewFromEnv() factory
│   ├── models/             Shared data types (Task, Plan, Artifact, Review, …)
│   ├── orchestrator/       Workflow engine (sequencing, routing, retry, stop)
│   ├── prompts/            Template loader (filesystem override + embedded fallback)
│   │   └── templates/      Embedded default prompt templates
│   └── state/              Thread-safe shared workflow state
├── prompts/                User-editable prompt templates (override embedded)
├── tests/
│   ├── unit/               Unit tests for each layer
│   └── integration/        End-to-end pipeline tests (mock provider)
├── .env.example            Environment variable reference
├── Makefile                Build, run, test targets
└── README.md               This file
```

---

## Adding a new agent

1. Create `internal/agents/myagent.go`:

```go
package agents

import (
    "context"
    "fmt"

    "mugi/internal/llm"
    "mugi/internal/prompts"
    "mugi/internal/state"
)

type MyAgent struct {
    provider llm.Provider
    loader   *prompts.Loader
}

func NewMyAgent(provider llm.Provider, loader *prompts.Loader) *MyAgent {
    return &MyAgent{provider: provider, loader: loader}
}

func (a *MyAgent) Role() string { return "my-agent" }

func (a *MyAgent) Process(ctx context.Context, st *state.WorkflowState) error {
    sysPrompt, _ := a.loader.Render("my-agent", st.Task)
    resp, err := a.provider.Generate(ctx, llm.Request{
        SystemPrompt: sysPrompt,
        Messages:     []llm.Message{{Role: "user", Content: "go"}},
        MaxTokens:    2048,
    })
    if err != nil {
        return fmt.Errorf("my-agent: %w", err)
    }
    st.AddLog("my-agent", resp.Content)
    return nil
}
```

2. Add `prompts/my-agent.tmpl` with the system prompt.
3. Wire the agent into `orchestrator.New(...)` in `cmd/mugi/main.go`.

---

## Adding a new LLM provider

Implement the `llm.Provider` interface:

```go
type MyProvider struct{}

func (p *MyProvider) Name() string { return "myprovider/model" }

func (p *MyProvider) Generate(ctx context.Context, req llm.Request) (llm.Response, error) {
    // call your API here
    return llm.Response{Content: "..."}, nil
}
```

Add a case to `llm.NewFromEnv()` in `internal/llm/registry.go`.

---

## License

MIT
