# MuGi — an execution-grounded benchmark for code-generation agents

MuGi runs code-generation agents against well-specified programming tasks,
**compiles and tests every artifact they produce**, scores it against
**held-out tests the agent never sees**, and reports resolution rate, cost, and
latency. It ships a multi-agent pipeline (Coordinator → Planner → Coder →
Reviewer) as the reference agent — but the interesting part isn't the agent, it's
measuring *honestly* whether that orchestration is worth its cost, and catching
the ways LLM-driven workflows quietly lie about their own quality.

It's written in Go, is provider-agnostic (Anthropic, OpenAI-compatible, Ollama,
or a deterministic mock), and runs the whole pipeline offline with no API key.

---

## Headline results

One run, two frontier models, two orchestration strategies, every artifact built
and tested (`go run ./cmd/bench -providers sonnet,haiku -strategy both`, 2026-06-24):

| Provider | Strategy | Build | Test (self) | Hidden | False approvals | Avg latency | Cost | $/solved |
|---|---|---:|---:|---:|---:|---:|---:|---:|
| `claude-haiku-4-5` | pipeline | 12/12 | 11/12 | 5/5 | **5** | 79.0s | $0.64 | $0.058 |
| `claude-haiku-4-5` | single   | 11/12 |  7/12 | 5/5 | 0 | 27.0s | $0.22 | $0.031 |
| `claude-sonnet-4-6` | pipeline | 12/12 | 10/12 | 5/5 | **4** | 130.3s | $1.43 | $0.143 |
| `claude-sonnet-4-6` | single   | 12/12 | **11/12** | 5/5 | 0 | 26.9s | $0.38 | $0.034 |

*Total run cost: $2.66. Full per-task detail and captured failure output:*
[`bench/results/RESULTS.md`](bench/results/RESULTS.md) · *raw rows:*
[`bench/results/results.csv`](bench/results/results.csv) · *provenance:*
[`bench/results/run.json`](bench/results/run.json).

---

## What the harness measured

> **Fuller write-up:** [Does multi-agent orchestration beat a single LLM call? I measured it.](docs/does-multi-agent-orchestration-help.md)

### 1. The LLM reviewer rubber-stamps code that fails its own tests

Across the two pipeline runs, the reviewer agent **approved failing artifacts 9
times** (Haiku 5, Sonnet 4) — scoring them ~9/10 *while `go test` on that same
artifact was red*, and it had been handed the failing output. Without an
independent gate, every one of those would have shipped as "approved."

MuGi computes `build`/`test` itself and an **objective gate** overrides the
reviewer whenever execution is red, so a rubber-stamped artifact still surfaces as
a failure and the loop keeps revising. The lesson is the headline: **you cannot
delegate the quality gate to an LLM reviewer, even when you feed it the test
results.** Eyeballing one "looks-right" program by hand reproduces exactly this
blind spot — only scoring execution across every artifact exposes it.

### 2. Does the multi-agent pipeline beat a single call? Only for the weak model.

Same model, same tasks — the full 4-agent pipeline vs. one well-prompted Coder
call (`-strategy both`):

- **Haiku: the pipeline genuinely helped — 11/12 vs 7/12** — but at **~3× the
  cost.** Orchestration compensates for a less capable model.
- **Sonnet: the single call won — 11/12 vs 10/12 — at ~¼ the cost** ($0.38 vs
  $1.43) and ⅕ the latency. (One of the pipeline's two misses was an API error,
  not the model; even being charitable, the pipeline did no better.) On the
  Pub/Sub task specifically, the pipeline's revision loop **took a solution a
  single call got right and broke it**, then approved the broken version 3 times.

**Cost per solved task tells the story: Sonnet-single $0.034, Sonnet-pipeline
$0.143** — the orchestration is dominated for a capable model. The honest takeaway:
*multi-agent orchestration buys capability for a weak model and little or nothing
for a strong one, always at 3–4× the cost.* If your model is good, a single
well-prompted call is the better engineering choice on tasks this size.

### 3. Held-out tests confirm the wins are real (not self-graded)

`test (self)` runs the model's **own** tests — and a model can pass by writing
weak tests. So 5 tasks carry a **`hidden_test`**: a harness-authored acceptance
test the coder never sees, injected into the implementation and run separately.
Every passing solution on those 5 tasks also passed its hidden test (**5/5 across
all four configs**), so the self-reported passes there aren't gamed. Extending
hidden coverage to the hard tasks — where the failures cluster — is the next step.

> The harness keeps **infrastructure failures distinct from model failures**: an
> `unexpected EOF` from the API after retry/backoff is recorded as an error row,
> never as a build/test failure, so a dropped connection is never miscounted as
> "the model can't solve this."

---

## How it works

The reference agent is the multi-agent pipeline — but it's just *one row* in the
benchmark above, not the point of the project.

```
User prompt
    │
    ▼
┌─────────────┐
│ Coordinator │  Initialises the workflow, narrates progress, final summary
└──────┬──────┘
       ▼
┌─────────────┐
│   Planner   │  Turns the task into a dependency-ordered execution plan
└──────┬──────┘
       ▼                   ┌──────────┐
┌─────────────┐  build+   │ Reviewer │  Inspects the artifact + the real
│    Coder    │  test ──► │          │  go build / go test output
└─────────────┘  ◄─────── └──────────┘
   (loop ≤ MAX_REVISIONS, but an OBJECTIVE GATE can override a red "approval")
```

- The **orchestrator** is pure Go: it sequences agents, runs `go build`/`go test`
  on every artifact, enforces the revision cap, and applies the objective gate.
  Agents never know about each other — they share a lock-guarded `WorkflowState`.
- The **`-strategy single`** path skips the plan/review/revision entirely: one
  Coder call, same objective scoring — the control for the ablation above.
- The **LLM layer** is a single `Provider` interface; every agent calls only
  `provider.Generate(ctx, req)`. Swapping mock → Anthropic → Ollama is one env var.
- **Untrusted by default:** generated code is built and tested with credential
  environment variables stripped, and every model-supplied file path is contained
  to the run directory.

---

## Real repository-level tasks (SWE-bench Lite)

The Go tasks above are greenfield. To measure on *real* repository changes, MuGi
also integrates [SWE-bench](https://www.swebench.com/) Lite — apply a candidate
patch plus the held-out `test_patch` to a real repo checkout, run the tests, and
score the `FAIL_TO_PASS` / `PASS_TO_PASS` contract.

- The **core** (`internal/swebench`) — dataset loader, pytest/go-test log parsers,
  scoring, and an `Environment` seam — is unit-tested offline against a local git
  fixture (gold patch resolves, empty doesn't, garbage doesn't apply).
- The **authoritative `% resolved` comes from the official SWE-bench harness**, not
  a home-grown evaluator (home-grown harnesses are a known source of
  non-reproducible numbers). MuGi's job is to produce predictions; the maintainers'
  harness scores them.
- [`.github/workflows/swebench.yml`](.github/workflows/swebench.yml) runs the
  official harness on the **gold patches** for a small Lite slice on GitHub Actions
  — a $0, no-model-calls validation that the whole rig works end to end.

See [`internal/swebench/README.md`](internal/swebench/README.md) for the design.

---

## Quickstart (no API key required)

```bash
go run ./cmd/mugi "Build a simple Go HTTP server with a /health endpoint"
```

The default `mock` provider returns deterministic responses so the full pipeline
runs offline. Run the benchmark yourself:

```bash
go run ./cmd/bench -providers mock                 # offline harness self-test
go run ./cmd/bench -providers haiku -strategy both # needs ANTHROPIC_API_KEY
```

The benchmark streams each row to `results.csv` as it finishes (crash-safe) and
writes a `run.json` provenance stamp (git SHA, models, Go version). How to run,
add tasks, or add providers: [`bench/README.md`](bench/README.md).

---

## Metrics

| Column | Meaning |
|---|---|
| **build** | `go build ./...` passed on the final artifact |
| **test (self)** | `go test ./...` passed on the model's *own* tests (and it shipped tests) |
| **hidden** | passed a held-out, harness-authored acceptance test the coder never saw |
| **false approvals** | times the reviewer approved an artifact whose build/test was red |
| **$/solved** | total cost ÷ tasks resolved — the number that actually compares models |

Every number is reproducible from `bench/results/` and stamped in `run.json`; a row
that can't be run cleanly is marked unrun, never back-filled.

---

## Configuration

Copy `.env.example` to `.env` and set what you need:

| Variable | Default | Description |
|---|---|---|
| `LLM_PROVIDER` | `mock` | `mock` \| `anthropic` \| `openai` \| `ollama` |
| `LLM_MODEL` | provider default | Model name override |
| `MAX_REVISIONS` | `3` | Max coder→reviewer cycles |
| `MAX_LLM_CALLS` | `50` | Hard ceiling on calls per run (cost guardrail) |
| `OUTPUT_DIR` | `output` | Where artifact files are written |

```bash
LLM_PROVIDER=anthropic ANTHROPIC_API_KEY=sk-ant-... \
  go run ./cmd/mugi "Build a REST API in Go"
```

Providers: **Anthropic** (`claude-sonnet-4-6`, `claude-haiku-4-5-20251001`,
`claude-opus-4-7`), **OpenAI-compatible** (set `OPENAI_BASE_URL` for Groq /
Together / Mistral / …), and **Ollama** (local, `OLLAMA_BASE_URL`). Prompt
templates live in `prompts/*.tmpl` and are editable without recompiling.

---

## Project layout

```
cmd/
  mugi/             CLI entrypoint (run the pipeline on one task)
  bench/            Evaluation harness — see bench/README.md
internal/
  agents/           Coordinator, Planner, Coder, Reviewer, + single-call SoloCoder
  orchestrator/     Workflow engine: sequencing, objective gate, single-call runner
  runner/           Builds/tests artifacts + runs held-out tests (secrets scrubbed)
  swebench/         SWE-bench-compatible eval core — see internal/swebench/README.md
  llm/              Provider interface + adapters (mock, anthropic, openai, ollama)
  fsafe/            Path-containment helpers for untrusted file paths
  models/  state/  prompts/  config/
bench/
  tasks/            One YAML per task (spec + optional hidden_test)
  results/          CSV + JSON + Markdown + run.json (regenerated each run)
.github/workflows/  CI (build/vet/race/gofmt/mock-bench) + SWE-bench gold validation
```

---

## Running the tests

```bash
make test         # all tests, mock provider, no API key
make test-race    # with the race detector (as CI does)
make bench-mock   # the harness's own golden baseline
```

## License

MIT
