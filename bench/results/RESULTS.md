# MuGi bench results

Generated 2026-05-24 20:11 UTC · total wall-clock: 1m52s · 1 providers × 12 tasks

## Provider rollup

| Provider | Build pass | Test pass | Avg score | Avg revisions | Avg latency | Total cost |
|---|---:|---:|---:|---:|---:|---:|
| `mock` | 12/12 | 12/12 | 9.0 | 0.0 | 9.3s | $0.0000 |

**Build pass** = `go build ./...` succeeded on the final artifact. 
**Test pass** = `go test ./...` also succeeded AND the artifact contained at least one `*_test.go` file. 
**Avg score** = mean of the reviewer's final 0–10 score. 
**Avg revisions** = mean coder→reviewer cycles used (0 = approved first try; max = revision cap hit).

## Per-task detail

### Easy

| Task | Provider | Build | Test | Score | Revs | Latency | Cost |
|---|---|:---:|:---:|---:|---:|---:|---:|
| FizzBuzz | `mock` | ✓ | ✓ | 9/10 | 0 | 9.5s | $0.0000 |
| Unicode-Safe String Reverse | `mock` | ✓ | ✓ | 9/10 | 0 | 9.2s | $0.0000 |
| Memoized Fibonacci | `mock` | ✓ | ✓ | 9/10 | 0 | 9.2s | $0.0000 |
| HTTP Health Endpoint | `mock` | ✓ | ✓ | 9/10 | 0 | 9.3s | $0.0000 |

### Medium

| Task | Provider | Build | Test | Score | Revs | Latency | Cost |
|---|---|:---:|:---:|---:|---:|---:|---:|
| Token-Bucket Rate Limiter | `mock` | ✓ | ✓ | 9/10 | 0 | 9.1s | $0.0000 |
| LRU Cache | `mock` | ✓ | ✓ | 9/10 | 0 | 9.4s | $0.0000 |
| JSON Config Loader with Defaults | `mock` | ✓ | ✓ | 9/10 | 0 | 9.6s | $0.0000 |
| CSV-to-JSON Converter | `mock` | ✓ | ✓ | 9/10 | 0 | 9.7s | $0.0000 |
| In-Memory URL Shortener | `mock` | ✓ | ✓ | 9/10 | 0 | 8.9s | $0.0000 |

### Hard

| Task | Provider | Build | Test | Score | Revs | Latency | Cost |
|---|---|:---:|:---:|---:|---:|---:|---:|
| In-Memory Pub/Sub with Per-Subscriber Buffer | `mock` | ✓ | ✓ | 9/10 | 0 | 9.2s | $0.0000 |
| Worker Pool with Graceful Shutdown | `mock` | ✓ | ✓ | 9/10 | 0 | 9.3s | $0.0000 |
| Arithmetic Expression Evaluator | `mock` | ✓ | ✓ | 9/10 | 0 | 9.3s | $0.0000 |

Raw data: [`results.csv`](results.csv) · [`summary.json`](summary.json)
