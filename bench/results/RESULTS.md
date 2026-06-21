# MuGi bench results

Generated 2026-06-21 16:00 UTC · total wall-clock: 31m20s · 3 providers × 12 tasks

## Provider rollup

| Provider | Build pass | Test pass | Avg score | Avg revisions | Avg latency | Total cost |
|---|---:|---:|---:|---:|---:|---:|
| `mock` | 12/12 | 12/12 | 9.0 | 0.0 | 8.5s | $0.0000 |
| `anthropic/claude-haiku-4-5-20251001` | 12/12 | 8/12 | 9.2 | 0.2 | 50.2s | $0.4972 |
| `anthropic/claude-sonnet-4-6` | 11/12 | 10/12 | 9.4 | 0.0 | 97.9s | $1.0176 |

**Build pass** = `go build ./...` succeeded on the final artifact. 
**Test pass** = `go test ./...` also succeeded AND the artifact contained at least one `*_test.go` file. 
**Avg score** = mean of the reviewer's final 0–10 score. 
**Avg revisions** = mean coder→reviewer cycles used (0 = approved first try; max = revision cap hit).

## Per-task detail

### Easy

| Task | Provider | Build | Test | Score | Revs | Latency | Cost |
|---|---|:---:|:---:|---:|---:|---:|---:|
| FizzBuzz | `mock` | ✓ | ✓ | 9/10 | 0 | 10.1s | $0.0000 |
| FizzBuzz | `anthropic/claude-haiku-4-5-20251001` | ✓ | ✓ | 10/10 | 0 | 23.5s | $0.0192 |
| FizzBuzz | `anthropic/claude-sonnet-4-6` | ✓ | ✓ | 10/10 | 0 | 41.6s | $0.0530 |
| Unicode-Safe String Reverse | `mock` | ✓ | ✓ | 9/10 | 0 | 8.3s | $0.0000 |
| Unicode-Safe String Reverse | `anthropic/claude-haiku-4-5-20251001` | ✓ | ✓ | 10/10 | 0 | 22.1s | $0.0149 |
| Unicode-Safe String Reverse | `anthropic/claude-sonnet-4-6` | ✓ | ✓ | 10/10 | 0 | 42.8s | $0.0471 |
| Memoized Fibonacci | `mock` | ✓ | ✓ | 9/10 | 0 | 8.4s | $0.0000 |
| Memoized Fibonacci | `anthropic/claude-haiku-4-5-20251001` | ✓ | ✓ | 9/10 | 0 | 26.6s | $0.0203 |
| Memoized Fibonacci | `anthropic/claude-sonnet-4-6` | ✓ | ✓ | 9/10 | 0 | 50.7s | $0.0565 |
| HTTP Health Endpoint | `mock` | ✓ | ✓ | 9/10 | 0 | 8.4s | $0.0000 |
| HTTP Health Endpoint | `anthropic/claude-haiku-4-5-20251001` | ✓ | ✓ | 9/10 | 0 | 31.2s | $0.0170 |
| HTTP Health Endpoint | `anthropic/claude-sonnet-4-6` | ✓ | ✓ | 10/10 | 0 | 48.6s | $0.0517 |

### Medium

| Task | Provider | Build | Test | Score | Revs | Latency | Cost |
|---|---|:---:|:---:|---:|---:|---:|---:|
| Token-Bucket Rate Limiter | `mock` | ✓ | ✓ | 9/10 | 0 | 8.1s | $0.0000 |
| Token-Bucket Rate Limiter | `anthropic/claude-haiku-4-5-20251001` | ✓ | ✓ | 9/10 | 1 | 68.0s | $0.0508 |
| Token-Bucket Rate Limiter | `anthropic/claude-sonnet-4-6` | ✓ | ✓ | 9/10 | 0 | 76.5s | $0.0782 |
| LRU Cache | `mock` | ✓ | ✓ | 9/10 | 0 | 8.2s | $0.0000 |
| LRU Cache | `anthropic/claude-haiku-4-5-20251001` | ✓ | ✗ | 9/10 | 0 | 46.0s | $0.0406 |
| LRU Cache | `anthropic/claude-sonnet-4-6` | ✓ | ✓ | 10/10 | 0 | 87.9s | $0.1111 |
| JSON Config Loader with Defaults | `mock` | ✓ | ✓ | 9/10 | 0 | 8.4s | $0.0000 |
| JSON Config Loader with Defaults | `anthropic/claude-haiku-4-5-20251001` | ✓ | ✓ | 9/10 | 0 | 35.1s | $0.0263 |
| JSON Config Loader with Defaults | `anthropic/claude-sonnet-4-6` | ✓ | ✓ | 9/10 | 0 | 80.7s | $0.1009 |
| CSV-to-JSON Converter | `mock` | ✓ | ✓ | 9/10 | 0 | 8.3s | $0.0000 |
| CSV-to-JSON Converter | `anthropic/claude-haiku-4-5-20251001` | ✓ | ✓ | 9/10 | 1 | 63.0s | $0.0509 |
| CSV-to-JSON Converter | `anthropic/claude-sonnet-4-6` | ✓ | ✓ | 9/10 | 0 | 74.2s | $0.0843 |
| In-Memory URL Shortener | `mock` | ✓ | ✓ | 9/10 | 0 | 8.0s | $0.0000 |
| In-Memory URL Shortener | `anthropic/claude-haiku-4-5-20251001` | ✓ | ✓ | 9/10 | 0 | 65.9s | $0.0661 |
| In-Memory URL Shortener | `anthropic/claude-sonnet-4-6` | ✓ | ✓ | 9/10 | 0 | 85.2s | $0.1018 |

### Hard

| Task | Provider | Build | Test | Score | Revs | Latency | Cost |
|---|---|:---:|:---:|---:|---:|---:|---:|
| In-Memory Pub/Sub with Per-Subscriber Buffer | `mock` | ✓ | ✓ | 9/10 | 0 | 8.5s | $0.0000 |
| In-Memory Pub/Sub with Per-Subscriber Buffer | `anthropic/claude-haiku-4-5-20251001` | ✓ | ✗ | 9/10 | 0 | 52.1s | $0.0439 |
| In-Memory Pub/Sub with Per-Subscriber Buffer | `anthropic/claude-sonnet-4-6` | ✓ | ✓ | 9/10 | 0 | 112.1s | $0.1367 |
| Worker Pool with Graceful Shutdown | `mock` | ✓ | ✓ | 9/10 | 0 | 8.4s | $0.0000 |
| Worker Pool with Graceful Shutdown | `anthropic/claude-haiku-4-5-20251001` | ✓ | ✗ | 9/10 | 1 | 112.5s | $0.0906 |
| Worker Pool with Graceful Shutdown | `anthropic/claude-sonnet-4-6` | 💥 | 💥 | — | 0 | 354.8s | $0.0427 |
| Arithmetic Expression Evaluator | `mock` | ✓ | ✓ | 9/10 | 0 | 8.5s | $0.0000 |
| Arithmetic Expression Evaluator | `anthropic/claude-haiku-4-5-20251001` | ✓ | ✗ | 9/10 | 0 | 56.6s | $0.0565 |
| Arithmetic Expression Evaluator | `anthropic/claude-sonnet-4-6` | ✓ | ✗ | 9/10 | 0 | 120.4s | $0.1537 |

## Failure analysis

Each failure below is grounded in the artifact's real `go build` / `go test` output. Mock is the all-green harness baseline and is excluded.

### `anthropic/claude-haiku-4-5-20251001` — In-Memory Pub/Sub with Per-Subscriber Buffer

- **Stage:** built, but `go test` failed
- **Revisions used:** 0 · **reviewer score:** 9/10 (approved=true)
- **Evidence (captured output):**

```
# example.com/pubsub [example.com/pubsub.test]
.\pubsub_test.go:235:4: declared and not used: ch
FAIL	example.com/pubsub [build failed]
FAIL
```

### `anthropic/claude-haiku-4-5-20251001` — Worker Pool with Graceful Shutdown

- **Stage:** built, but `go test` failed
- **Revisions used:** 1 · **reviewer score:** 9/10 (approved=true)
- **Evidence (captured output):**

```
--- FAIL: TestShutdownWithAmpleDeadline (5.10s)
    pool_test.go:131: Shutdown with ample deadline should return nil, got context deadline exceeded
FAIL
FAIL	example.com/pool	5.590s
FAIL
```

### `anthropic/claude-haiku-4-5-20251001` — Arithmetic Expression Evaluator

- **Stage:** built, but `go test` failed
- **Revisions used:** 0 · **reviewer score:** 9/10 (approved=true)
- **Evidence (captured output):**

```
--- FAIL: TestEval (0.00s)
    --- FAIL: TestEval/leading_decimal (0.00s)
        expr_test.go:251: Eval(".5+.5") unexpected error: unknown character: .
        expr_test.go:254: Eval(".5+.5") = 0, want 1
    --- FAIL: TestEval/error:_two_operators_in_a_row (0.00s)
        expr_test.go:247: Eval("1++2") expected error, got nil
    --- FAIL: TestEval/error:_operator_at_start (0.00s)
        expr_test.go:254: Eval("+1") = 1, want 0
    --- FAIL: TestEval/multiple_parentheses_with_operations (0.00s)
        expr_test.go:254: Eval("(1+2)*(3+4)-(5+6)") = 10, want 14
FAIL
FAIL	example.com/expr	0.386...[truncated]
```

### `anthropic/claude-haiku-4-5-20251001` — LRU Cache

- **Stage:** built, but `go test` failed
- **Revisions used:** 0 · **reviewer score:** 9/10 (approved=true)
- **Evidence (captured output):**

```
--- FAIL: TestMultipleTypes (0.00s)
    lru_test.go:197: Get(1) should return false after eviction
FAIL
FAIL	example.com/lru	0.377s
FAIL
```

### `anthropic/claude-sonnet-4-6` — Worker Pool with Graceful Shutdown

- **Stage:** errored before a buildable artifact — the pipeline could not parse the model's output
- **Revisions used:** 0
- **Evidence (captured output):**

```
[coder/code] coder: llm: anthropic: transient network error: Post "https://api.anthropic.com/v1/messages": unexpected EOF
```

### `anthropic/claude-sonnet-4-6` — Arithmetic Expression Evaluator

- **Stage:** built, but `go test` failed
- **Revisions used:** 0 · **reviewer score:** 9/10 (approved=true)
- **Evidence (captured output):**

```
--- FAIL: TestEval (0.00s)
    --- FAIL: TestEval/-1_*_-1 (0.00s)
        expr_test.go:65: Eval("-1 * -1") = 1, nil; want error
FAIL
FAIL	example.com/expr	0.471s
FAIL
```

Raw data: [`results.csv`](results.csv) · [`summary.json`](summary.json)
