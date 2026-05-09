# MuGi — Multi-Agent Software Builder

MuGi is a **Go-native, provider-agnostic multi-agent pipeline** that turns a
one-line task description into a structured plan, working code, and a
peer-reviewed result — all driven by four collaborating LLM agents.

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
├── cmd/mugi/               CLI entrypoint
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
