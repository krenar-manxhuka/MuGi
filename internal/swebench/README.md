# swebench — a SWE-bench-compatible evaluation core

This package evaluates a candidate patch against a real [SWE-bench](https://www.swebench.com/)
instance: apply the patch, apply the held-out `test_patch`, run the tests, and
score the `FAIL_TO_PASS` / `PASS_TO_PASS` contract. It exists so MuGi can be
measured on a recognized, repository-level benchmark — not just the greenfield
Go tasks in `bench/`.

## Design: a pure core behind an execution seam

The package is split so the part that needs heavy infrastructure (real repos,
Python, Docker) is isolated behind one interface, and everything else is
unit-testable offline:

| Piece | File | Needs network/Docker? |
|---|---|---|
| Instance schema + dataset loader (JSONL / array, stringified lists) | `instance.go` | no |
| Test-log parsers (`pytest`, `go test`) | `report.go` | no |
| Scoring (`FAIL_TO_PASS` must flip; `PASS_TO_PASS` must hold) | `report.go` | no |
| Eval orchestration (apply → test → score) | `eval.go` | no |
| **`Environment`** — *where* patches apply and commands run | `env.go` | the seam |

`Environment` has one offline implementation today, `LocalEnv`, which runs in a
temp git checkout. A `DockerEnv` implementing the same two methods (`Apply`,
`Run`) will let the identical eval flow execute real SWE-bench instances inside
their per-instance containers — without changing any scoring logic.

## What's validated offline (no spend, no Docker, runs in CI)

`eval_test.go` builds a tiny local git repo with a deliberately buggy function
and runs the same baselines the real harness uses, via `go test` instead of
pytest:

- **gold patch → resolved** (the held-out test flips failing → passing),
- **empty-diff baseline → not resolved** (the held-out test still fails),
- **garbage patch → reported as not applied** (and unresolved).

These are the harness's own correctness tests: if the gold patch doesn't resolve
or the empty patch does, the harness is broken, and CI says so.

## Authoritative evaluation: the official harness on GitHub Actions

Applying a model's patch and running a real repo's test suite executes untrusted
code and pulls multi-GB images, so the real-instance evaluation runs on ephemeral
CI runners inside Docker, never on a dev machine — see
[`.github/workflows/swebench.yml`](../../.github/workflows/swebench.yml).

Crucially, the **authoritative `% resolved` comes from the official SWE-bench
harness** (`python -m swebench.harness.run_evaluation`), not a home-grown
evaluator. Reimplementing SWE-bench's per-instance environments and scoring is the
classic source of inflated, non-reproducible numbers; using the maintainers'
images and harness keeps MuGi's results comparable to published SWE-bench. So the
division of labour is:

- **MuGi (Go)** = the agent: turn an instance's problem statement into a unified
  diff (a "prediction").
- **Official harness (Python, Docker, Actions)** = the evaluator.
- **This package** = dataset loading/slicing, the offline gold/empty sanity
  baselines (`LocalEnv`), and our own reporting. `LocalEnv` is the local,
  no-Docker path; a `DockerEnv` could be added later as a *secondary* cross-check,
  but the headline numbers are the official harness's.

The workflow's first job evaluates the **gold patches** for a tiny slice — $0 in
model spend — and fails unless every gold patch resolves, turning the rig's own
correctness into a CI gate.

## Status

- ✅ Core: schema, loader, parsers, scoring, eval flow, `LocalEnv` — built and
  offline-validated.
- ✅ `workflow_dispatch` Actions job: official harness, gold-patch validation on a
  configurable SWE-bench Lite slice ($0).
- ✅ Retrieval recall@k harness ([`internal/retrievaleval`](../retrievaleval),
  `cmd/swebench-recall`): tune retrieval against gold patches for $0.
- ✅ Agent diff-output contract ([`internal/predict`](../predict),
  `cmd/swebench-predict`): MuGi produces a unified-diff prediction per instance,
  offline-tested through the mock provider. Pointing it at a real model and
  feeding `predictions.jsonl` to the official harness is the step that costs API
  tokens.
