# MuGi bench results

Generated 2026-05-24 20:17 UTC · total wall-clock: 5m6s · 1 providers × 3 tasks

## Provider rollup

| Provider | Build pass | Test pass | Avg score | Avg revisions | Avg latency | Total cost |
|---|---:|---:|---:|---:|---:|---:|
| `ollama/qwen3:1.7b` | 0/3 | 0/3 | 9.0 | 0.0 | 102.1s | $0.0000 |

**Build pass** = `go build ./...` succeeded on the final artifact. 
**Test pass** = `go test ./...` also succeeded AND the artifact contained at least one `*_test.go` file. 
**Avg score** = mean of the reviewer's final 0–10 score. 
**Avg revisions** = mean coder→reviewer cycles used (0 = approved first try; max = revision cap hit).

## Per-task detail

### Easy

| Task | Provider | Build | Test | Score | Revs | Latency | Cost |
|---|---|:---:|:---:|---:|---:|---:|---:|
| FizzBuzz | `ollama/qwen3:1.7b` | ✗ | ✗ | 9/10 | 0 | 83.8s | $0.0000 |

### Medium

| Task | Provider | Build | Test | Score | Revs | Latency | Cost |
|---|---|:---:|:---:|---:|---:|---:|---:|
| Token-Bucket Rate Limiter | `ollama/qwen3:1.7b` | 💥 | 💥 | — | 0 | 95.5s | $0.0000 |

### Hard

| Task | Provider | Build | Test | Score | Revs | Latency | Cost |
|---|---|:---:|:---:|---:|---:|---:|---:|
| In-Memory Pub/Sub with Per-Subscriber Buffer | `ollama/qwen3:1.7b` | ✗ | ✗ | — | 0 | 127.0s | $0.0000 |

Raw data: [`results.csv`](results.csv) · [`summary.json`](summary.json)
