# predict — instance → unified diff prediction

Turns a SWE-bench task plus the retrieved code into a **prediction**: one unified
diff the official harness can apply and score. This is the agent half of M3 —
retrieval (`internal/retrievaleval`) finds the code, this asks a model to fix it.

## The contract

- **One well-prompted call**, not a pipeline. The bench ablation
  ([`docs/does-multi-agent-orchestration-help.md`](../../docs/does-multi-agent-orchestration-help.md))
  found a 4-agent loop doesn't beat a single good call for a strong model, and the
  reviewer rubber-stamps failing code anyway.
- **Diff, not files.** The model is told to emit only a `git apply`-able unified
  diff. `ExtractDiff` unwraps a ```` ```diff ```` fence or leading prose and
  `ValidateDiff` checks the shape; the harness's real `git apply` is the arbiter.
- **Every instance gets a row.** A provider error or an unusable reply yields an
  empty-patch `Prediction` (a valid "unresolved" row), so the predictions file
  always matches the slice — failures are recorded, never dropped.

## What's here

| Piece | File | Notes |
|---|---|---|
| `Task`, `Prediction`, `Result`, `Config`, `Generator.Predict` | `predict.go` | the single-call generator |
| Prompt assembly (bounded context, plain-text excerpt markers) | `prompt.go`, `templates/swebench_diff.tmpl` | template is user-overridable via `PROMPTS_DIR` |
| `ExtractDiff` / `ValidateDiff` — pull a diff out of a model reply | `diff.go` | guard, not a parser |
| `WritePredictions` — JSON Lines in the harness's schema | `writer.go` | `{instance_id, model_name_or_path, model_patch}` |

Everything is offline-testable through the `llm.Provider` seam with the mock
provider — **no spend**. `Result.PlausibleHit` (does the diff touch a file
retrieval actually showed?) is a free sanity signal recorded for every prediction.

## Running it

`cmd/swebench-predict` wires checkout → retrieval → this generator → predictions
file. The model is `LLM_PROVIDER` (default **mock**, $0); only an explicit real
provider spends:

```bash
# offline dry run — proves the pipeline, $0:
go run ./cmd/swebench-predict -instances slice.jsonl -mode lexical -k 10

# real run — SPENDS on the chosen API:
LLM_PROVIDER=anthropic ANTHROPIC_API_KEY=... \
  go run ./cmd/swebench-predict -instances slice.jsonl -mode lexical -k 10
```

The resulting `predictions.jsonl` is what the official harness scores (swap it in
for `--predictions_path gold`). It clones public repos — run it on CI/sandbox.

## The ablation: is it retrieval or the model?

`-context` controls what code the model sees, so resolution can be reported under
three conditions that separate the two failure modes:

| `-context` | What the model sees | Reads |
|---|---|---|
| `none` | nothing (the report alone) | lower bound — no checkout, no network |
| `retrieval` | top-k retrieved chunks | the real system |
| `oracle` | exactly the gold patch's changed files | upper bound — isolates generation |

If `retrieval ≈ oracle`, retrieval is solved and the gap to `oracle` is the model's
headroom; if `oracle` resolves but `retrieval` doesn't, the fix is better retrieval.
The `.github/workflows/swebench-predict.yml` workflow takes a `context` input, so
the full ablation is three dispatches. `oracle` only uses the gold patch to *pick
the files*; the model is never shown the diff.
