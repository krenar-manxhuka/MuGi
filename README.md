# MuGi — Multi-Agent Software Builder

MuGi is a **Go-native, provider-agnostic multi-agent pipeline** that turns a
one-line task description into a structured plan, working code, and a
peer-reviewed result — all driven by four collaborating LLM agents. Every
generated artifact is compiled and tested before the reviewer sees it, so the
review is grounded in real execution output, not LLM speculation.

---

## Benchmark — execution-grounded evaluation

The heart of this repo is an **eval harness** (`cmd/bench`) that runs the full
pipeline against 12 small Go tasks (4 easy / 5 medium / 3 hard), each with
explicit function signatures and required test cases, and **compiles and tests
every generated artifact** before scoring it. The goal isn't a leaderboard — it
is to surface *where and how* models fail, grounded in real `go build` /
`go test` output rather than an LLM's self-assessment.

Real numbers from a single run on **2026-06-21**
(`go run ./cmd/bench -providers mock,haiku,sonnet`):

| Provider | Build pass | Test pass | Avg score | Avg revisions | Avg latency | Total cost |
|---|---:|---:|---:|---:|---:|---:|
| `mock` (harness baseline) | 12/12 | 12/12 | 9.0 | 0.0 | 8.5s | $0.0000 |
| `anthropic/claude-haiku-4-5-20251001` | 12/12 | 8/12 | 9.2 | 0.2 | 50.2s | $0.4972 |
| `anthropic/claude-sonnet-4-6` | 11/12 † | 10/12 | 9.4 | 0.0 | 97.9s | $1.0176 |
| `ollama/qwen2.5-coder:7b` | _not run this cycle ‡_ | | | | | |

Full per-task detail and the **captured failure output** for every miss:
[`bench/results/RESULTS.md`](bench/results/RESULTS.md) · raw rows:
[`bench/results/results.csv`](bench/results/results.csv) · how to run:
[`bench/README.md`](bench/README.md).

> **† Sonnet's one non-pass was infrastructure, not the model.** The Worker Pool
> task errored with `unexpected EOF` from the Anthropic API after the provider's
> retry/backoff exhausted (~5 min). The harness records it as a 💥 error row,
> kept distinct from build/test failures (Failure mode #3). Sonnet built all 11
> other tasks and passed tests on 10 of them — only the Expr Evaluator missed,
> on a single `-1 * -1` edge case.
>
> **‡ Ollama is left unrun rather than filled with stale numbers.** A full local
> run with `qwen2.5-coder:7b` OOM-killed the host on the reviewer's large-context
> call. Integrity rule for this table: every number comes from a run reproducible
> from `bench/results/`; a row that can't be run cleanly is marked unrun, not
> back-filled. To populate it, run the reviewer with a smaller context (or on a
> larger-RAM machine) and `LLM_MODEL=qwen2.5-coder:7b go run ./cmd/bench -providers ollama`.

### Metrics

- **Build pass** — `go build ./...` succeeded on the final artifact.
- **Test pass** — `go test ./...` also passed *and* the artifact shipped at least
  one `*_test.go` file (so "wrote no tests" cannot score as a pass).
- **Avg score** — mean of the reviewer agent's final 0–10 score.
- **Avg revisions** — mean coder→reviewer cycles used (0 = approved first try; 3 = hit the cap).
- **Avg latency** — wall-clock per task, end to end (inference + `go build` + `go test`).
- **Total cost** — summed token usage × per-model price (the `pricing` table in `cmd/bench/main.go`).

### Failure modes the harness has caught

Each item is a methodology point — what the eval surfaced, the root cause, and
why manual testing would have missed it.

**1. The LLM reviewer approves code that fails its own tests.** In the 2026-06-21
run, *every* Haiku artifact that failed `go test` was nonetheless **approved by
the reviewer at 9/10** — and so was Sonnet's one test failure:

| Task | Model | Reviewer verdict | What `go test` actually caught |
|---|---|---|---|
| Pub/Sub | haiku | approved · 9/10 | test file won't compile: `declared and not used: ch` |
| Worker Pool | haiku | approved · 9/10 | `Shutdown` exceeds its deadline (`context deadline exceeded`) |
| Expr Evaluator | haiku | approved · 9/10 | 4 correctness bugs: `.5` lexing, `1++2` not rejected, unary `+1`, wrong precedence |
| LRU Cache | haiku | approved · 9/10 | eviction bug: `Get(1) should return false after eviction` |
| Expr Evaluator | sonnet | approved · 9/10 | `-1 * -1` evaluates instead of erroring per the task contract |

Root cause: the reviewer **is handed the `go build` / `go test` output** (the
orchestrator runs the artifact before the review), but its holistic "this looks
like correct Go" judgment overrides the explicit failing signal — it approved all
five at 9/10 anyway. **Why this matters:** you cannot delegate the quality gate to
the LLM reviewer *even when you feed it the test results*. The harness therefore
computes `build_ok` / `test_ok` itself, independently of the reviewer score, so a
rubber-stamped artifact still surfaces as test ✗. (Eyeballing one "looks-right"
program by hand reproduces exactly the reviewer's blind spot — only scoring
execution across every artifact exposes it.) Full captured output for every row
above: [`bench/results/RESULTS.md`](bench/results/RESULTS.md).

**2. `runner.detectLang` silently skipped execution for every artifact.** An
earlier version read only the *first* file's `Lang` field — which for any Go
module is `go.mod` with `lang: "text"` — so it returned `"text"`, found no
matching executor, and marked every artifact `Skipped`: build and test never ran.
The eval surfaced it because build/test results were absent across *all* real
artifacts at once, not in any single case. Root cause: detection keyed on
`files[0]` instead of scanning for any `.go` file; fixed in
[internal/runner/runner.go](internal/runner/runner.go) (any `.go` file ⇒ run as
Go). **Why manual testing misses it:** a hand-run of one generated program
compiles fine — the bug only shows up as a systematic "nothing is being executed"
pattern across the whole suite, which is exactly what a harness makes visible.

**3. Infrastructure failures are kept distinct from model failures.** Sonnet's
Worker Pool task errored with `unexpected EOF` from the Anthropic API after the
provider's retry/backoff gave up (~5 min). The harness records that as a 💥 error
row with no artifact — never as a build or test failure — so a dropped connection
is never miscounted as "the model can't solve worker pools." A naive harness that
lumped the two together would report a misleading capability number.

**Local models (`qwen2.5-coder:7b`).** A full local run OOM-killed the host on the
reviewer's large-context call — the reviewer prompt embeds the full artifact JSON,
which is heavy for a 7B model on commodity hardware — which is why that row is left
unrun. In isolated single-task smoke tests the same model produced *correct*
FizzBuzz logic but omitted its `fmt` and `reflect` imports, so it would not
compile: another "looks right, doesn't build" case that only execution catches.
(Smoke-test observations, not a scored benchmark row.)

> **The mock row is a contract test for the harness, not a model benchmark.** The
> mock returns the same canned artifact for every task, so its 12/12 verifies that
> the orchestrator, executor, and scorer agree end to end — the "all green"
> baseline that proves the rig itself works.

### Limitations & next steps

An honest snapshot, not a finished eval platform. Known gaps, in priority order:

1. **Single run, no repeated trials.** Model failures shift run to run, so a
   one-shot pass rate conflates capability with nondeterminism. Next: a `-trials N`
   flag reporting pass-rate mean ± range and flagging flaky tasks.
2. **"Test pass" runs the model's own tests.** The coder writes both the code and
   its tests, which can be jointly weak or wrong. Next: hold out a hidden,
   harness-authored acceptance test per task and score against that — the strongest
   credibility upgrade.
3. **Execution is Go-only.** Non-Go artifacts are reported `skipped` (no signal).
   Next: per-language runners, or scope the harness explicitly to Go.
4. **Results persist only at the end of a run.** A crash mid-run loses completed
   rows. Next: append each row to `results.csv` as it finishes so long runs survive
   a failure.
5. **Local-model row is unrun.** `qwen2.5-coder:7b` OOM'd the host on the reviewer's
   large-context call. Next: shrink the reviewer context for local models, then run it.

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
  solo.tmpl          ← single-call baseline (one shot, no plan/review)
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
