# MuGi bench results

Generated 2026-06-24 18:15 UTC · total wall-clock: 53m39s · 2 providers × 12 tasks

## Provider rollup

| Provider | Strategy | Build pass | Test pass (self) | Hidden pass | Avg score | Avg revisions | False approvals | Avg latency | Total cost |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `anthropic/claude-haiku-4-5-20251001` | pipeline | 12/12 | 11/12 | 5/5 | 9.3 | 0.8 | 5 | 79.0s | $0.6371 |
| `anthropic/claude-haiku-4-5-20251001` | single | 11/12 | 7/12 | 5/5 | 0.0 | 0.0 | 0 | 27.0s | $0.2159 |
| `anthropic/claude-sonnet-4-6` | pipeline | 12/12 | 10/12 | 5/5 | 9.2 | 0.4 | 4 | 130.3s | $1.4272 |
| `anthropic/claude-sonnet-4-6` | single | 12/12 | 11/12 | 5/5 | 0.0 | 0.0 | 0 | 26.9s | $0.3764 |

**Strategy** = `pipeline` (Coordinator→Planner→Coder→Reviewer with a revision loop) or `single` (one Coder call, no plan/review/revision). Running both is the ablation for whether the extra agents actually beat one well-prompted call — same provider, same tasks, same objective scoring. 
**Build pass** = `go build ./...` succeeded on the final artifact. 
**Test pass (self)** = `go test ./...` passed AND the artifact shipped a `*_test.go` file — i.e. the model's *own* tests passed. 
**Hidden pass** = the implementation passed a held-out, harness-authored acceptance test the coder never saw (over the tasks that have one). This is the metric that cannot be gamed: where **Hidden pass < Test pass (self)**, the model wrote tests too weak to catch its own bugs. `—` means no task in that row carried a hidden test. 
**Avg score** = mean of the reviewer's final 0–10 score (single-call rows have no reviewer, shown as 0.0). 
**Avg revisions** = mean coder→reviewer cycles used (0 = approved first try; max = revision cap hit; always 0 for single). 
**False approvals** = times the reviewer approved an artifact whose `go build`/`go test` was still red, forcing the orchestrator's objective gate to override the approval and keep revising. A non-zero count means the LLM reviewer cannot be trusted as the sole quality gate — the whole reason build/test is scored independently.

## Per-task detail

### Easy

| Task | Provider | Strategy | Build | Test | Hidden | Score | Revs | Latency | Cost |
|---|---|---|:---:|:---:|:---:|---:|---:|---:|---:|
| FizzBuzz | `anthropic/claude-haiku-4-5-20251001` | pipeline | ✓ | ✓ | ✓ | 10/10 | 0 | 20.6s | $0.0156 |
| FizzBuzz | `anthropic/claude-haiku-4-5-20251001` | single | ✓ | ✓ | ✓ | — | 0 | 8.3s | $0.0053 |
| FizzBuzz | `anthropic/claude-sonnet-4-6` | pipeline | ✓ | ✓ | ✓ | 9/10 | 0 | 52.6s | $0.0600 |
| FizzBuzz | `anthropic/claude-sonnet-4-6` | single | ✓ | ✓ | ✓ | — | 0 | 14.1s | $0.0172 |
| Unicode-Safe String Reverse | `anthropic/claude-haiku-4-5-20251001` | pipeline | ✓ | ✓ | ✓ | 10/10 | 0 | 30.8s | $0.0151 |
| Unicode-Safe String Reverse | `anthropic/claude-haiku-4-5-20251001` | single | ✓ | ✓ | ✓ | — | 0 | 8.5s | $0.0050 |
| Unicode-Safe String Reverse | `anthropic/claude-sonnet-4-6` | pipeline | ✓ | ✓ | ✓ | 10/10 | 0 | 50.1s | $0.0509 |
| Unicode-Safe String Reverse | `anthropic/claude-sonnet-4-6` | single | ✓ | ✓ | ✓ | — | 0 | 10.1s | $0.0098 |
| Memoized Fibonacci | `anthropic/claude-haiku-4-5-20251001` | pipeline | ✓ | ✓ | ✓ | 9/10 | 1 | 49.0s | $0.0349 |
| Memoized Fibonacci | `anthropic/claude-haiku-4-5-20251001` | single | ✓ | ✗ | ✓ | — | 0 | 7.4s | $0.0053 |
| Memoized Fibonacci | `anthropic/claude-sonnet-4-6` | pipeline | ✓ | ✓ | ✓ | 9/10 | 0 | 49.2s | $0.0567 |
| Memoized Fibonacci | `anthropic/claude-sonnet-4-6` | single | ✓ | ✓ | ✓ | — | 0 | 11.6s | $0.0112 |
| HTTP Health Endpoint | `anthropic/claude-haiku-4-5-20251001` | pipeline | ✓ | ✓ | — | 10/10 | 1 | 46.0s | $0.0264 |
| HTTP Health Endpoint | `anthropic/claude-haiku-4-5-20251001` | single | ✓ | ✓ | — | — | 0 | 10.9s | $0.0056 |
| HTTP Health Endpoint | `anthropic/claude-sonnet-4-6` | pipeline | ✓ | ✓ | — | 9/10 | 0 | 53.2s | $0.0553 |
| HTTP Health Endpoint | `anthropic/claude-sonnet-4-6` | single | ✓ | ✓ | — | — | 0 | 16.9s | $0.0140 |

### Medium

| Task | Provider | Strategy | Build | Test | Hidden | Score | Revs | Latency | Cost |
|---|---|---|:---:|:---:|:---:|---:|---:|---:|---:|
| Token-Bucket Rate Limiter | `anthropic/claude-haiku-4-5-20251001` | pipeline | ✓ | ✓ | — | 9/10 | 1 | 55.4s | $0.0439 |
| Token-Bucket Rate Limiter | `anthropic/claude-haiku-4-5-20251001` | single | ✓ | ✓ | — | — | 0 | 22.7s | $0.0179 |
| Token-Bucket Rate Limiter | `anthropic/claude-sonnet-4-6` | pipeline | ✓ | ✓ | — | 9/10 | 0 | 73.3s | $0.0835 |
| Token-Bucket Rate Limiter | `anthropic/claude-sonnet-4-6` | single | ✓ | ✓ | — | — | 0 | 21.7s | $0.0235 |
| LRU Cache | `anthropic/claude-haiku-4-5-20251001` | pipeline | ✓ | ✓ | ✓ | 10/10 | 0 | 47.4s | $0.0444 |
| LRU Cache | `anthropic/claude-haiku-4-5-20251001` | single | ✓ | ✓ | ✓ | — | 0 | 15.5s | $0.0125 |
| LRU Cache | `anthropic/claude-sonnet-4-6` | pipeline | ✓ | ✓ | ✓ | 10/10 | 0 | 76.5s | $0.0948 |
| LRU Cache | `anthropic/claude-sonnet-4-6` | single | ✓ | ✓ | ✓ | — | 0 | 40.1s | $0.0494 |
| JSON Config Loader with Defaults | `anthropic/claude-haiku-4-5-20251001` | pipeline | ✓ | ✓ | — | 9/10 | 0 | 43.9s | $0.0338 |
| JSON Config Loader with Defaults | `anthropic/claude-haiku-4-5-20251001` | single | ✓ | ✓ | — | — | 0 | 14.2s | $0.0112 |
| JSON Config Loader with Defaults | `anthropic/claude-sonnet-4-6` | pipeline | ✓ | ✓ | — | 9/10 | 0 | 80.5s | $0.0973 |
| JSON Config Loader with Defaults | `anthropic/claude-sonnet-4-6` | single | ✓ | ✓ | — | — | 0 | 23.0s | $0.0312 |
| CSV-to-JSON Converter | `anthropic/claude-haiku-4-5-20251001` | pipeline | ✓ | ✓ | — | 9/10 | 1 | 60.6s | $0.0523 |
| CSV-to-JSON Converter | `anthropic/claude-haiku-4-5-20251001` | single | ✓ | ✗ | — | — | 0 | 13.8s | $0.0103 |
| CSV-to-JSON Converter | `anthropic/claude-sonnet-4-6` | pipeline | ✓ | ✓ | — | 9/10 | 0 | 78.3s | $0.0918 |
| CSV-to-JSON Converter | `anthropic/claude-sonnet-4-6` | single | ✓ | ✓ | — | — | 0 | 23.1s | $0.0275 |
| In-Memory URL Shortener | `anthropic/claude-haiku-4-5-20251001` | pipeline | ✓ | ✓ | ✓ | 9/10 | 0 | 39.4s | $0.0330 |
| In-Memory URL Shortener | `anthropic/claude-haiku-4-5-20251001` | single | ✓ | ✓ | ✓ | — | 0 | 17.2s | $0.0138 |
| In-Memory URL Shortener | `anthropic/claude-sonnet-4-6` | pipeline | ✓ | ✓ | ✓ | 9/10 | 0 | 83.8s | $0.1071 |
| In-Memory URL Shortener | `anthropic/claude-sonnet-4-6` | single | ✓ | ✓ | ✓ | — | 0 | 28.0s | $0.0366 |

### Hard

| Task | Provider | Strategy | Build | Test | Hidden | Score | Revs | Latency | Cost |
|---|---|---|:---:|:---:|:---:|---:|---:|---:|---:|
| In-Memory Pub/Sub with Per-Subscriber Buffer | `anthropic/claude-haiku-4-5-20251001` | pipeline | ✓ | ✗ | — | 9/10 | 3 | 298.0s | $0.1051 |
| In-Memory Pub/Sub with Per-Subscriber Buffer | `anthropic/claude-haiku-4-5-20251001` | single | ✓ | ✗ | — | — | 0 | 139.7s | $0.0546 |
| In-Memory Pub/Sub with Per-Subscriber Buffer | `anthropic/claude-sonnet-4-6` | pipeline | ✓ | ✗ | — | 9/10 | 3 | 238.8s | $0.3010 |
| In-Memory Pub/Sub with Per-Subscriber Buffer | `anthropic/claude-sonnet-4-6` | single | ✓ | ✓ | — | — | 0 | 37.7s | $0.0457 |
| Worker Pool with Graceful Shutdown | `anthropic/claude-haiku-4-5-20251001` | pipeline | ✓ | ✓ | — | 9/10 | 2 | 167.1s | $0.1448 |
| Worker Pool with Graceful Shutdown | `anthropic/claude-haiku-4-5-20251001` | single | ✗ | ✗ | — | — | 0 | 44.0s | $0.0539 |
| Worker Pool with Graceful Shutdown | `anthropic/claude-sonnet-4-6` | pipeline | ✓ | ✗ | — | 9/10 | 1 | 545.5s | $0.1718 |
| Worker Pool with Graceful Shutdown | `anthropic/claude-sonnet-4-6` | single | ✓ | ✗ | — | — | 0 | 55.4s | $0.0570 |
| Arithmetic Expression Evaluator | `anthropic/claude-haiku-4-5-20251001` | pipeline | ✓ | ✓ | — | 9/10 | 1 | 89.3s | $0.0877 |
| Arithmetic Expression Evaluator | `anthropic/claude-haiku-4-5-20251001` | single | ✓ | ✗ | — | — | 0 | 21.3s | $0.0205 |
| Arithmetic Expression Evaluator | `anthropic/claude-sonnet-4-6` | pipeline | ✓ | ✓ | — | 9/10 | 1 | 181.6s | $0.2571 |
| Arithmetic Expression Evaluator | `anthropic/claude-sonnet-4-6` | single | ✓ | ✓ | — | — | 0 | 41.3s | $0.0532 |

## Failure analysis

Each failure below is grounded in the artifact's real `go build` / `go test` output. Mock is the all-green harness baseline and is excluded.

### `anthropic/claude-haiku-4-5-20251001` (pipeline) — In-Memory Pub/Sub with Per-Subscriber Buffer

- **Stage:** built, but `go test` failed
- **Revisions used:** 3 · **reviewer score:** 9/10 (approved=true)

### `anthropic/claude-haiku-4-5-20251001` (single) — Memoized Fibonacci

- **Stage:** built, but `go test` failed
- **Revisions used:** 0
- **Evidence (captured output):**

```
# example.com/fib [example.com/fib.test]
.\fib_test.go:23:5: t.Run undefined (type struct{n int; expected uint64} has no field or method Run)
FAIL	example.com/fib [build failed]
FAIL
```

### `anthropic/claude-haiku-4-5-20251001` (single) — In-Memory Pub/Sub with Per-Subscriber Buffer

- **Stage:** built, but `go test` failed
- **Revisions used:** 0

### `anthropic/claude-haiku-4-5-20251001` (single) — Worker Pool with Graceful Shutdown

- **Stage:** `go build` failed — the model emitted uncompilable Go
- **Revisions used:** 0
- **Evidence (captured output):**

```
# example.com/pool
.\pool.go:67:21: cannot use wrappedJob (variable of type func(ctx context.Context)) as Job value in send
```

### `anthropic/claude-haiku-4-5-20251001` (single) — Arithmetic Expression Evaluator

- **Stage:** built, but `go test` failed
- **Revisions used:** 0
- **Evidence (captured output):**

```
--- FAIL: TestEval (0.00s)
    --- FAIL: TestEval/two_operators (0.00s)
        expr_test.go:50: Eval("1 + + 2") error = <nil>, wantErr = true
FAIL
FAIL	example.com/expr	0.382s
FAIL
```

### `anthropic/claude-haiku-4-5-20251001` (single) — CSV-to-JSON Converter

- **Stage:** built, but `go test` failed
- **Revisions used:** 0
- **Evidence (captured output):**

```
--- FAIL: TestConvertHeaderOrder (0.00s)
    csv2json_test.go:73: expected [{"z":"1","a":"2","m":"3"}], got [{"a":"2","m":"3","z":"1"}]
FAIL
FAIL	example.com/csv2json	0.460s
FAIL
```

### `anthropic/claude-sonnet-4-6` (pipeline) — In-Memory Pub/Sub with Per-Subscriber Buffer

- **Stage:** built, but `go test` failed
- **Revisions used:** 3 · **reviewer score:** 9/10 (approved=true)
- **Evidence (captured output):**

```
--- FAIL: TestCloseClosesAllChannels (0.00s)
panic: close of closed channel [recovered, repanicked]

goroutine 10 [running]:
testing.tRunner.func1.2({0x7ff631373280, 0x7ff6313b00d0})
	C:/Program Files/Go/src/testing/testing.go:1974 +0x239
testing.tRunner.func1()
	C:/Program Files/Go/src/testing/testing.go:1977 +0x349
panic({0x7ff631373280?, 0x7ff6313b00d0?})
	C:/Program Files/Go/src/runtime/panic.go:860 +0x13a
example.com/pubsub.(*Broker[...]).Subscribe.func2.1()
	C:<tmpdir>/mugi-run-XXXX/pubsub.go:69 +0x5f
sync.(*Once).doSlow(0x0?, 0x7ff6313aecf8?)
	C:/Program Files/Go/src/sync/once.go:78 +0x...[truncated]
```

### `anthropic/claude-sonnet-4-6` (pipeline) — Worker Pool with Graceful Shutdown

- **Stage:** built, but `go test` failed
- **Revisions used:** 1 · **reviewer score:** 9/10 (approved=true)

### `anthropic/claude-sonnet-4-6` (single) — Worker Pool with Graceful Shutdown

- **Stage:** built, but `go test` failed
- **Revisions used:** 0
- **Evidence (captured output):**

```
--- FAIL: TestShutdownCancelsInflightJobs (5.00s)
    pool_test.go:126: Shutdown: context deadline exceeded
FAIL
FAIL	example.com/pool	7.615s
FAIL
```

Raw data: [`results.csv`](results.csv) · [`summary.json`](summary.json)
