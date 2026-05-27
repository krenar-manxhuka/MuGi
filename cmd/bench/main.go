// Command bench runs the MuGi pipeline against a suite of YAML-defined tasks
// across one or more LLM providers, then writes a CSV + Markdown report.
//
// Usage:
//
//	go run ./cmd/bench                        # all providers, all tasks
//	go run ./cmd/bench -providers mock        # mock only (no API key needed)
//	go run ./cmd/bench -quick                 # smoke run: 1 task per tier
//	go run ./cmd/bench -tasks bench/tasks/easy-*.yaml
package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"mugi/internal/agents"
	"mugi/internal/llm"
	"mugi/internal/models"
	"mugi/internal/orchestrator"
	"mugi/internal/prompts"
)

// --- pricing (USD per 1M tokens) ------------------------------------------
//
// Update these if Anthropic changes published pricing. Mock is free.
// Values as of late 2025 / early 2026.

type priceCard struct {
	inputPerMTok  float64
	outputPerMTok float64
}

var pricing = map[string]priceCard{
	"mock":                        {0, 0},
	"anthropic/claude-sonnet-4-6": {3.00, 15.00},
	"anthropic/claude-haiku-4-5":  {1.00, 5.00},
	// Ollama models run locally and are billed at $0/MTok. Latency != free.
	"ollama/":                     {0, 0},
}

// --- task model -----------------------------------------------------------

type Task struct {
	ID          string `yaml:"id"`
	Name        string `yaml:"name"`
	Tier        string `yaml:"tier"` // easy | medium | hard
	Description string `yaml:"description"`
}

// --- benchmark result -----------------------------------------------------

type Row struct {
	TaskID        string  `json:"task_id"`
	Tier          string  `json:"tier"`
	TaskName      string  `json:"task_name"`
	Provider      string  `json:"provider"`
	BuildOK       bool    `json:"build_ok"`
	TestsPresent  bool    `json:"tests_present"`
	TestOK        bool    `json:"test_ok"`
	ReviewerScore int     `json:"reviewer_score"` // -1 if no review
	Approved      bool    `json:"approved"`
	RevisionsUsed int     `json:"revisions_used"`
	DurationMs    int64   `json:"duration_ms"`
	InputTokens   int     `json:"input_tokens"`
	OutputTokens  int     `json:"output_tokens"`
	LLMCalls      int     `json:"llm_calls"`
	CostUSD       float64 `json:"cost_usd"`
	BuildOut      string  `json:"build_out,omitempty"` // populated when BuildOK is false
	TestOut       string  `json:"test_out,omitempty"`  // populated when TestOK is false
	Error         string  `json:"error,omitempty"`
}

// --- counting provider ----------------------------------------------------

// renamingProvider gives an existing Provider a different Name(). Used so
// Ollama (which speaks OpenAI's protocol) shows up as "ollama/<model>" in
// the report instead of "openai/<model>", which would clash with real
// OpenAI's pricing lookup.
type renamingProvider struct {
	inner llm.Provider
	name  string
}

func (r *renamingProvider) Name() string { return r.name }
func (r *renamingProvider) Generate(ctx context.Context, req llm.Request) (llm.Response, error) {
	return r.inner.Generate(ctx, req)
}

// countingProvider wraps any Provider, tallying cumulative token usage and
// call count. Safe for concurrent use but the bench drives it serially.
type countingProvider struct {
	inner llm.Provider
	mu    sync.Mutex
	in    int
	out   int
	calls int
}

func newCounting(inner llm.Provider) *countingProvider {
	return &countingProvider{inner: inner}
}

func (c *countingProvider) Name() string { return c.inner.Name() }

func (c *countingProvider) Generate(ctx context.Context, req llm.Request) (llm.Response, error) {
	resp, err := c.inner.Generate(ctx, req)
	c.mu.Lock()
	c.calls++
	c.in += resp.Usage.InputTokens
	c.out += resp.Usage.OutputTokens
	c.mu.Unlock()
	return resp, err
}

func (c *countingProvider) snapshot() (in, out, calls int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.in, c.out, c.calls
}

// --- providers under test --------------------------------------------------

// providerSpec is the friendly name (CLI/CSV) plus a constructor that may
// return nil to indicate the provider can't be used in this environment
// (e.g., missing API key) along with a reason string.
type providerSpec struct {
	cliName string
	build   func() (llm.Provider, string)
}

func availableProviders() map[string]providerSpec {
	return map[string]providerSpec{
		"mock": {
			cliName: "mock",
			build: func() (llm.Provider, string) {
				return llm.NewMockProvider(), ""
			},
		},
		"sonnet": {
			cliName: "sonnet",
			build: func() (llm.Provider, string) {
				k := os.Getenv("ANTHROPIC_API_KEY")
				if k == "" {
					return nil, "ANTHROPIC_API_KEY not set"
				}
				return llm.NewAnthropicProvider(k, "claude-sonnet-4-6"), ""
			},
		},
		"haiku": {
			cliName: "haiku",
			build: func() (llm.Provider, string) {
				k := os.Getenv("ANTHROPIC_API_KEY")
				if k == "" {
					return nil, "ANTHROPIC_API_KEY not set"
				}
				return llm.NewAnthropicProvider(k, "claude-haiku-4-5-20251001"), ""
			},
		},
		"ollama": {
			cliName: "ollama",
			build: func() (llm.Provider, string) {
				baseURL := os.Getenv("OLLAMA_BASE_URL")
				if baseURL == "" {
					baseURL = "http://localhost:11434/v1"
				}
				model := os.Getenv("LLM_MODEL")
				if model == "" || strings.HasPrefix(model, "claude") || strings.HasPrefix(model, "gpt") {
					model = "llama3"
				}
				// 10-minute per-call timeout: local models on consumer
				// hardware are slow, and our reviewer prompts are large.
				inner := llm.NewOpenAIProvider(baseURL, "", model, 10*time.Minute, 0)
				return &renamingProvider{inner: inner, name: "ollama/" + model}, ""
			},
		},
	}
}

// --- main ------------------------------------------------------------------

func main() {
	loadDotEnv(".env")

	tasksGlob := flag.String("tasks", "bench/tasks/*.yaml", "glob for task YAML files")
	providersCSV := flag.String("providers", "mock,sonnet,haiku", "comma-separated provider names")
	outDir := flag.String("out", "bench/results", "output directory for CSV / Markdown")
	maxRev := flag.Int("max-revisions", 3, "max coder/reviewer cycles per task")
	quick := flag.Bool("quick", false, "smoke run: one task per tier")
	verbose := flag.Bool("v", false, "stream orchestrator logs to stderr")
	flag.Parse()

	logLevel := slog.LevelError
	if *verbose {
		logLevel = slog.LevelInfo
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	tasks, err := loadTasks(*tasksGlob)
	if err != nil {
		die("load tasks: %v", err)
	}
	if *quick {
		tasks = oneTaskPerTier(tasks)
	}
	if len(tasks) == 0 {
		die("no tasks matched %q", *tasksGlob)
	}

	provNames := splitCSV(*providersCSV)
	provs, skipped := resolveProviders(provNames)
	if len(provs) == 0 {
		die("no usable providers (requested: %v)", provNames)
	}
	for name, reason := range skipped {
		fmt.Fprintf(os.Stderr, "SKIP provider %q: %s\n", name, reason)
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		die("mkdir %s: %v", *outDir, err)
	}

	loader := prompts.NewLoader("prompts")

	totalRuns := len(tasks) * len(provs)
	fmt.Fprintf(os.Stderr, "running %d tasks × %d providers = %d runs (maxRevisions=%d)\n\n",
		len(tasks), len(provs), totalRuns, *maxRev)

	var rows []Row
	startedAll := time.Now()
	completed := 0

	for _, pName := range orderedProviderNames(provs) {
		p := provs[pName]
		for _, t := range tasks {
			completed++
			fmt.Fprintf(os.Stderr, "[%d/%d] %s × %s ... ", completed, totalRuns, pName, t.ID)

			row := runOnce(t, p, loader, *maxRev)
			rows = append(rows, row)

			outcome := "ok"
			if row.Error != "" {
				outcome = "ERR"
			}
			fmt.Fprintf(os.Stderr, "%s  build=%v test=%v score=%d rev=%d %dms $%.4f\n",
				outcome, row.BuildOK, row.TestOK, row.ReviewerScore,
				row.RevisionsUsed, row.DurationMs, row.CostUSD)
		}
	}

	totalElapsed := time.Since(startedAll).Round(time.Second)
	fmt.Fprintf(os.Stderr, "\ndone in %s\n", totalElapsed)

	if err := writeCSV(filepath.Join(*outDir, "results.csv"), rows); err != nil {
		die("write CSV: %v", err)
	}
	if err := writeJSON(filepath.Join(*outDir, "summary.json"), rows); err != nil {
		die("write JSON: %v", err)
	}
	if err := writeMarkdown(filepath.Join(*outDir, "RESULTS.md"), rows, totalElapsed); err != nil {
		die("write Markdown: %v", err)
	}

	fmt.Fprintf(os.Stderr, "wrote %s/{results.csv, summary.json, RESULTS.md}\n", *outDir)
}

// --- run one task on one provider -----------------------------------------

func runOnce(t Task, inner llm.Provider, loader *prompts.Loader, maxRev int) Row {
	row := Row{
		TaskID:        t.ID,
		Tier:          t.Tier,
		TaskName:      t.Name,
		Provider:      inner.Name(),
		ReviewerScore: -1,
	}

	counter := newCounting(inner)

	orch := orchestrator.New(
		agents.NewCoordinator(counter, loader),
		agents.NewPlanner(counter, loader),
		agents.NewCoder(counter, loader),
		agents.NewReviewer(counter, loader),
		orchestrator.Config{
			MaxRevisions: maxRev,
			RunTests:     true,
			Logger:       slog.Default(),
		},
	)

	task := &models.Task{
		ID:          fmt.Sprintf("%s-%d", t.ID, time.Now().UnixMilli()),
		Description: t.Description,
		CreatedAt:   time.Now(),
	}

	// Per-run timeout: generous, so even a slow model doesn't hang the suite.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	start := time.Now()
	st, err := orch.Run(ctx, task)
	row.DurationMs = time.Since(start).Milliseconds()

	in, out, calls := counter.snapshot()
	row.InputTokens = in
	row.OutputTokens = out
	row.LLMCalls = calls
	row.CostUSD = computeCost(inner.Name(), in, out)

	if err != nil {
		row.Error = err.Error()
		return row
	}

	if exec := st.GetExecResult(); exec != nil {
		row.BuildOK = exec.BuildOK
		row.TestOK = exec.TestOK
		if !exec.BuildOK {
			row.BuildOut = truncate(exec.BuildOut, 4000)
		}
		if exec.BuildOK && !exec.TestOK {
			row.TestOut = truncate(exec.TestOut, 4000)
		}
	}
	if a := st.GetArtifact(); a != nil {
		for _, f := range a.Files {
			if strings.HasSuffix(f.Path, "_test.go") {
				row.TestsPresent = true
				break
			}
		}
	}
	if rev := st.LatestReview(); rev != nil {
		row.ReviewerScore = rev.Score
		row.Approved = rev.Approved
	}
	iter, _, _ := st.Snapshot()
	row.RevisionsUsed = iter

	return row
}

// --- pricing --------------------------------------------------------------

func computeCost(providerName string, inputTokens, outputTokens int) float64 {
	pc, ok := pricing[providerName]
	if !ok {
		// Unknown provider — try a best-effort match on the prefix.
		for k, v := range pricing {
			if strings.HasPrefix(providerName, k) {
				pc = v
				ok = true
				break
			}
		}
	}
	if !ok {
		return 0
	}
	return float64(inputTokens)/1_000_000*pc.inputPerMTok +
		float64(outputTokens)/1_000_000*pc.outputPerMTok
}

// --- task loading ---------------------------------------------------------

func loadTasks(glob string) ([]Task, error) {
	paths, err := filepath.Glob(glob)
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)

	var tasks []Task
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", p, err)
		}
		var t Task
		if err := yaml.Unmarshal(b, &t); err != nil {
			return nil, fmt.Errorf("parse %s: %w", p, err)
		}
		if t.ID == "" || t.Description == "" {
			return nil, fmt.Errorf("%s: id and description are required", p)
		}
		if t.Tier == "" {
			t.Tier = "unknown"
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}

func oneTaskPerTier(tasks []Task) []Task {
	seen := map[string]bool{}
	var out []Task
	for _, t := range tasks {
		if seen[t.Tier] {
			continue
		}
		seen[t.Tier] = true
		out = append(out, t)
	}
	return out
}

// --- provider resolution --------------------------------------------------

func resolveProviders(names []string) (map[string]llm.Provider, map[string]string) {
	avail := availableProviders()
	out := map[string]llm.Provider{}
	skipped := map[string]string{}
	for _, n := range names {
		spec, ok := avail[n]
		if !ok {
			skipped[n] = "unknown provider"
			continue
		}
		p, reason := spec.build()
		if p == nil {
			skipped[n] = reason
			continue
		}
		out[n] = p
	}
	return out, skipped
}

// orderedProviderNames returns the provider keys in a stable order:
// mock first (cheap baseline), then alphabetical for the rest.
func orderedProviderNames(provs map[string]llm.Provider) []string {
	var rest []string
	hasMock := false
	for k := range provs {
		if k == "mock" {
			hasMock = true
			continue
		}
		rest = append(rest, k)
	}
	sort.Strings(rest)
	if hasMock {
		return append([]string{"mock"}, rest...)
	}
	return rest
}

// --- writers --------------------------------------------------------------

func writeCSV(path string, rows []Row) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	header := []string{
		"task_id", "tier", "task_name", "provider",
		"build_ok", "tests_present", "test_ok",
		"reviewer_score", "approved", "revisions_used",
		"duration_ms", "input_tokens", "output_tokens",
		"llm_calls", "cost_usd", "error",
	}
	if err := w.Write(header); err != nil {
		return err
	}
	for _, r := range rows {
		err := w.Write([]string{
			r.TaskID, r.Tier, r.TaskName, r.Provider,
			strconv.FormatBool(r.BuildOK),
			strconv.FormatBool(r.TestsPresent),
			strconv.FormatBool(r.TestOK),
			strconv.Itoa(r.ReviewerScore),
			strconv.FormatBool(r.Approved),
			strconv.Itoa(r.RevisionsUsed),
			strconv.FormatInt(r.DurationMs, 10),
			strconv.Itoa(r.InputTokens),
			strconv.Itoa(r.OutputTokens),
			strconv.Itoa(r.LLMCalls),
			strconv.FormatFloat(r.CostUSD, 'f', 6, 64),
			r.Error,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func writeJSON(path string, rows []Row) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(rows)
}

// writeMarkdown renders two views: a per-task detail table grouped by tier,
// and a provider rollup with aggregate stats. The output is intended to be
// pasted into the project README.
func writeMarkdown(path string, rows []Row, totalElapsed time.Duration) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	providers := uniqueProviders(rows)
	sort.Slice(providers, func(i, j int) bool {
		if providers[i] == "mock" {
			return true
		}
		if providers[j] == "mock" {
			return false
		}
		return providers[i] < providers[j]
	})

	fmt.Fprintf(f, "# MuGi bench results\n\n")
	fmt.Fprintf(f, "Generated %s · total wall-clock: %s · %d providers × %d tasks\n\n",
		time.Now().UTC().Format("2006-01-02 15:04 UTC"),
		totalElapsed, len(providers), len(uniqueTaskIDs(rows)))

	// --- Provider rollup ---
	fmt.Fprintln(f, "## Provider rollup")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "| Provider | Build pass | Test pass | Avg score | Avg revisions | Avg latency | Total cost |")
	fmt.Fprintln(f, "|---|---:|---:|---:|---:|---:|---:|")
	for _, p := range providers {
		s := summarise(rows, p)
		fmt.Fprintf(f, "| `%s` | %d/%d | %d/%d | %.1f | %.1f | %s | $%.4f |\n",
			p,
			s.buildPass, s.total,
			s.testPass, s.total,
			s.avgScore, s.avgRevisions,
			fmtMs(s.avgDurationMs),
			s.totalCostUSD,
		)
	}
	fmt.Fprintln(f)
	fmt.Fprintln(f, "**Build pass** = `go build ./...` succeeded on the final artifact. ")
	fmt.Fprintln(f, "**Test pass** = `go test ./...` also succeeded AND the artifact contained at least one `*_test.go` file. ")
	fmt.Fprintln(f, "**Avg score** = mean of the reviewer's final 0–10 score. ")
	fmt.Fprintln(f, "**Avg revisions** = mean coder→reviewer cycles used (0 = approved first try; max = revision cap hit).")
	fmt.Fprintln(f)

	// --- Per-task detail ---
	fmt.Fprintln(f, "## Per-task detail")
	fmt.Fprintln(f)

	for _, tier := range []string{"easy", "medium", "hard"} {
		tierRows := filterTier(rows, tier)
		if len(tierRows) == 0 {
			continue
		}
		fmt.Fprintf(f, "### %s\n\n", strings.Title(tier)) //nolint:staticcheck // SA1019: deprecation acknowledged
		fmt.Fprintln(f, "| Task | Provider | Build | Test | Score | Revs | Latency | Cost |")
		fmt.Fprintln(f, "|---|---|:---:|:---:|---:|---:|---:|---:|")
		// Sort: by task id, then provider with mock first
		sort.SliceStable(tierRows, func(i, j int) bool {
			if tierRows[i].TaskID != tierRows[j].TaskID {
				return tierRows[i].TaskID < tierRows[j].TaskID
			}
			if tierRows[i].Provider == "mock" {
				return true
			}
			if tierRows[j].Provider == "mock" {
				return false
			}
			return tierRows[i].Provider < tierRows[j].Provider
		})
		for _, r := range tierRows {
			fmt.Fprintf(f, "| %s | `%s` | %s | %s | %s | %d | %s | $%.4f |\n",
				r.TaskName, r.Provider,
				tickFor(r.BuildOK, r.Error != ""),
				tickForTest(r),
				scoreCell(r),
				r.RevisionsUsed,
				fmtMs(float64(r.DurationMs)),
				r.CostUSD,
			)
		}
		fmt.Fprintln(f)
	}

	fmt.Fprintln(f, "Raw data: [`results.csv`](results.csv) · [`summary.json`](summary.json)")
	return nil
}

// --- aggregation helpers --------------------------------------------------

type summary struct {
	total                                    int
	buildPass, testPass                      int
	avgScore, avgRevisions, avgDurationMs    float64
	totalCostUSD                             float64
}

func summarise(rows []Row, provider string) summary {
	var s summary
	var scoreSum, scoreN int
	for _, r := range rows {
		if r.Provider != provider {
			continue
		}
		s.total++
		if r.BuildOK {
			s.buildPass++
		}
		if r.TestOK && r.TestsPresent {
			s.testPass++
		}
		if r.ReviewerScore >= 0 {
			scoreSum += r.ReviewerScore
			scoreN++
		}
		s.avgRevisions += float64(r.RevisionsUsed)
		s.avgDurationMs += float64(r.DurationMs)
		s.totalCostUSD += r.CostUSD
	}
	if s.total > 0 {
		s.avgRevisions /= float64(s.total)
		s.avgDurationMs /= float64(s.total)
	}
	if scoreN > 0 {
		s.avgScore = float64(scoreSum) / float64(scoreN)
	}
	return s
}

func uniqueProviders(rows []Row) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rows {
		if !seen[r.Provider] {
			seen[r.Provider] = true
			out = append(out, r.Provider)
		}
	}
	return out
}

func uniqueTaskIDs(rows []Row) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rows {
		if !seen[r.TaskID] {
			seen[r.TaskID] = true
			out = append(out, r.TaskID)
		}
	}
	return out
}

func filterTier(rows []Row, tier string) []Row {
	var out []Row
	for _, r := range rows {
		if r.Tier == tier {
			out = append(out, r)
		}
	}
	return out
}

func tickFor(ok, errored bool) string {
	if errored {
		return "💥"
	}
	if ok {
		return "✓"
	}
	return "✗"
}

func tickForTest(r Row) string {
	if r.Error != "" {
		return "💥"
	}
	if !r.TestsPresent {
		return "—"
	}
	if r.TestOK {
		return "✓"
	}
	return "✗"
}

func scoreCell(r Row) string {
	if r.ReviewerScore < 0 {
		return "—"
	}
	return strconv.Itoa(r.ReviewerScore) + "/10"
}

func fmtMs(ms float64) string {
	if ms < 1000 {
		return fmt.Sprintf("%.0fms", ms)
	}
	return fmt.Sprintf("%.1fs", ms/1000)
}

// --- small utilities ------------------------------------------------------

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "...[truncated]"
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "bench: "+format+"\n", args...)
	os.Exit(1)
}

// loadDotEnv mirrors cmd/mugi/main.go so the bench picks up ANTHROPIC_API_KEY
// from a project-root .env file without requiring a shell export.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		return
	}
	b, err := fs.ReadFile(os.DirFS(filepath.Dir(path)), filepath.Base(path))
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" || os.Getenv(k) != "" {
			continue
		}
		_ = os.Setenv(k, v)
	}
}
