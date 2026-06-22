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
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
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
	"mugi/internal/runner"
	"mugi/internal/state"
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
	"ollama/": {0, 0},
}

// --- task model -----------------------------------------------------------

type Task struct {
	ID          string `yaml:"id"`
	Name        string `yaml:"name"`
	Tier        string `yaml:"tier"` // easy | medium | hard
	Description string `yaml:"description"`
	// HiddenTest is an optional harness-authored acceptance test (Go test body,
	// no package clause) the coder never sees. When set, the bench runs it against
	// the implementation alone for a held-out signal independent of the model's
	// own tests.
	HiddenTest string `yaml:"hidden_test"`
}

// --- benchmark result -----------------------------------------------------

type Row struct {
	TaskID        string  `json:"task_id"`
	Tier          string  `json:"tier"`
	TaskName      string  `json:"task_name"`
	Provider      string  `json:"provider"`
	Strategy      string  `json:"strategy"` // pipeline | single
	BuildOK       bool    `json:"build_ok"`
	TestsPresent  bool    `json:"tests_present"`
	TestOK        bool    `json:"test_ok"`
	ReviewerScore int     `json:"reviewer_score"` // -1 if no review
	Approved      bool    `json:"approved"`
	FalseApproval int     `json:"false_approvals"` // reviewer approvals the objective gate overrode (build/test red)
	RevisionsUsed int     `json:"revisions_used"`
	DurationMs    int64   `json:"duration_ms"`
	InputTokens   int     `json:"input_tokens"`
	OutputTokens  int     `json:"output_tokens"`
	LLMCalls      int     `json:"llm_calls"`
	CostUSD       float64 `json:"cost_usd"`
	BuildOut      string  `json:"build_out,omitempty"` // populated when BuildOK is false
	TestOut       string  `json:"test_out,omitempty"`  // populated when TestOK is false
	// Held-out acceptance test (the coder never saw it). HiddenPresent is false
	// for tasks without one, in which case the other hidden fields are ignored.
	HiddenPresent bool   `json:"hidden_present"`
	HiddenBuildOK bool   `json:"hidden_build_ok"`
	HiddenTestOK  bool   `json:"hidden_test_ok"`
	HiddenOut     string `json:"hidden_out,omitempty"` // populated when the hidden test build/run fails
	Error         string `json:"error,omitempty"`
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
	strategyFlag := flag.String("strategy", "pipeline", "orchestration strategy: pipeline | single | both (ablation)")
	quick := flag.Bool("quick", false, "smoke run: one task per tier")
	verbose := flag.Bool("v", false, "stream orchestrator logs to stderr")
	keepArtifacts := flag.Bool("keep-artifacts", false, "write each run's generated files under <out>/_artifacts/ for inspection")
	flag.Parse()

	strategies, err := resolveStrategies(*strategyFlag)
	if err != nil {
		die("%v", err)
	}

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

	artifactsDir := ""
	if *keepArtifacts {
		// Leading underscore: the go tool ignores such dirs, so dumped
		// (often non-compiling) artifacts don't break `go build ./...`.
		artifactsDir = filepath.Join(*outDir, "_artifacts")
	}

	totalRuns := len(tasks) * len(provs) * len(strategies)
	fmt.Fprintf(os.Stderr, "running %d tasks × %d providers × %d strategies (%s) = %d runs (maxRevisions=%d)\n\n",
		len(tasks), len(provs), len(strategies), strings.Join(strategies, ","), totalRuns, *maxRev)

	// Stream rows to results.csv as they finish so a crash mid-run keeps the
	// rows completed so far. The JSON/Markdown reports are rendered at the end.
	streamer, err := newRowStreamer(filepath.Join(*outDir, "results.csv"))
	if err != nil {
		die("open results.csv for streaming: %v", err)
	}

	var rows []Row
	startedAll := time.Now()
	completed := 0

	for _, pName := range orderedProviderNames(provs) {
		p := provs[pName]
		for _, t := range tasks {
			for _, strat := range strategies {
				completed++
				fmt.Fprintf(os.Stderr, "[%d/%d] %s × %s × %s ... ", completed, totalRuns, pName, t.ID, strat)

				row := runOnce(t, p, loader, *maxRev, strat, artifactsDir)
				rows = append(rows, row)
				streamer.add(row)

				outcome := "ok"
				if row.Error != "" {
					outcome = "ERR"
				}
				fmt.Fprintf(os.Stderr, "%s  build=%v test=%v score=%d rev=%d fa=%d %dms $%.4f\n",
					outcome, row.BuildOK, row.TestOK, row.ReviewerScore,
					row.RevisionsUsed, row.FalseApproval, row.DurationMs, row.CostUSD)
			}
		}
	}
	streamer.close()

	totalElapsed := time.Since(startedAll).Round(time.Second)
	fmt.Fprintf(os.Stderr, "\ndone in %s\n", totalElapsed)

	sha, dirty := gitState()
	meta := runMeta{
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		GitSHA:        sha,
		GitDirty:      dirty,
		GoVersion:     runtime.Version(),
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		MaxRevisions:  *maxRev,
		Strategies:    strategies,
		TasksGlob:     *tasksGlob,
		TaskIDs:       taskIDs(tasks),
		Providers:     orderedProviderNames(provs),
		ProviderNames: providerModelNames(provs),
		TotalRuns:     len(rows),
		ElapsedSec:    totalElapsed.Seconds(),
	}
	if err := writeRunJSON(filepath.Join(*outDir, "run.json"), meta); err != nil {
		die("write run.json: %v", err)
	}
	if err := writeJSON(filepath.Join(*outDir, "summary.json"), rows); err != nil {
		die("write JSON: %v", err)
	}
	if err := writeMarkdown(filepath.Join(*outDir, "RESULTS.md"), rows, totalElapsed); err != nil {
		die("write Markdown: %v", err)
	}

	fmt.Fprintf(os.Stderr, "wrote %s/{results.csv, summary.json, RESULTS.md, run.json}\n", *outDir)
	if dirty {
		fmt.Fprintln(os.Stderr, "note: working tree was dirty at run time (run.json git_dirty=true)")
	}
}

// taskIDs returns the ids of the tasks actually run, for the provenance stamp.
func taskIDs(tasks []Task) []string {
	out := make([]string, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, t.ID)
	}
	return out
}

// providerModelNames maps each CLI provider name to the fully-qualified model
// name its Provider reports (e.g. "sonnet" → "anthropic/claude-sonnet-4-6"), so
// run.json records exactly which model produced each row.
func providerModelNames(provs map[string]llm.Provider) map[string]string {
	out := make(map[string]string, len(provs))
	for cli, p := range provs {
		out[cli] = p.Name()
	}
	return out
}

// --- strategies -----------------------------------------------------------

// resolveStrategies expands the -strategy flag into the list of orchestration
// strategies to run. "both" runs the full pipeline and the single-call baseline
// against every (task, provider) so they can be compared head to head.
func resolveStrategies(flagVal string) ([]string, error) {
	switch strings.ToLower(strings.TrimSpace(flagVal)) {
	case "", "pipeline":
		return []string{"pipeline"}, nil
	case "single", "solo":
		return []string{"single"}, nil
	case "both", "all":
		return []string{"pipeline", "single"}, nil
	default:
		return nil, fmt.Errorf("unknown -strategy %q (want pipeline | single | both)", flagVal)
	}
}

// runStrategy executes one task under the named orchestration strategy and
// returns the resulting workflow state. Both strategies share the same provider,
// task, and objective build/test scoring; they differ only in how the artifact
// is produced — the full Coordinator→Planner→Coder→Reviewer loop, or one
// single-call Coder.
func runStrategy(ctx context.Context, strategy string, provider llm.Provider, loader *prompts.Loader, maxRev int, task *models.Task) (*state.WorkflowState, error) {
	switch strategy {
	case "single":
		solo := orchestrator.NewSolo(agents.NewSoloCoder(provider, loader), true, slog.Default())
		return solo.Run(ctx, task)
	default: // "pipeline"
		orch := orchestrator.New(
			agents.NewCoordinator(provider, loader),
			agents.NewPlanner(provider, loader),
			agents.NewCoder(provider, loader),
			agents.NewReviewer(provider, loader),
			orchestrator.Config{
				MaxRevisions: maxRev,
				RunTests:     true,
				Logger:       slog.Default(),
			},
		)
		return orch.Run(ctx, task)
	}
}

// --- run one task on one provider -----------------------------------------

func runOnce(t Task, inner llm.Provider, loader *prompts.Loader, maxRev int, strategy string, artifactsDir string) Row {
	row := Row{
		TaskID:        t.ID,
		Tier:          t.Tier,
		TaskName:      t.Name,
		Provider:      inner.Name(),
		Strategy:      strategy,
		ReviewerScore: -1,
	}

	counter := newCounting(inner)

	task := &models.Task{
		ID:          fmt.Sprintf("%s-%d", t.ID, time.Now().UnixMilli()),
		Description: t.Description,
		CreatedAt:   time.Now(),
	}

	// Per-run timeout: generous, so even a slow model doesn't hang the suite.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	start := time.Now()
	st, err := runStrategy(ctx, strategy, counter, loader, maxRev, task)
	row.DurationMs = time.Since(start).Milliseconds()

	in, out, calls := counter.snapshot()
	row.InputTokens = in
	row.OutputTokens = out
	row.LLMCalls = calls
	row.CostUSD = computeCost(inner.Name(), in, out)

	if err != nil {
		row.Error = scrubPaths(err.Error())
		return row
	}

	if exec := st.GetExecResult(); exec != nil {
		row.BuildOK = exec.BuildOK
		row.TestOK = exec.TestOK
		if !exec.BuildOK {
			row.BuildOut = scrubPaths(truncate(exec.BuildOut, 4000))
		}
		if exec.BuildOK && !exec.TestOK {
			row.TestOut = scrubPaths(truncate(exec.TestOut, 4000))
		}
	}
	if a := st.GetArtifact(); a != nil {
		for _, f := range a.Files {
			if strings.HasSuffix(f.Path, "_test.go") {
				row.TestsPresent = true
				break
			}
		}
		if artifactsDir != "" {
			if err := dumpArtifact(artifactsDir, inner.Name(), t.ID, a); err != nil {
				fmt.Fprintf(os.Stderr, "  warn: dump artifact %s: %v\n", t.ID, err)
			}
		}

		// Held-out acceptance: score the implementation against the harness's own
		// hidden test (which the coder never saw), independent of the model's
		// self-authored tests. Run after the timed strategy so it doesn't inflate
		// the model's latency.
		if t.HiddenTest != "" {
			row.HiddenPresent = true
			hres := runner.RunHidden(ctx, a, t.HiddenTest, 90*time.Second)
			if !hres.Skipped {
				row.HiddenBuildOK = hres.BuildOK
				row.HiddenTestOK = hres.BuildOK && hres.TestOK
				switch {
				case !hres.BuildOK:
					row.HiddenOut = scrubPaths(truncate(hres.BuildOut, 4000))
				case !hres.TestOK:
					row.HiddenOut = scrubPaths(truncate(hres.TestOut, 4000))
				}
			}
		}
	}
	if rev := st.LatestReview(); rev != nil {
		row.ReviewerScore = rev.Score
		row.Approved = rev.Approved
	}
	row.FalseApproval = st.FalseApprovals()
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

// csvHeader is the column order for results.csv, shared by the streaming writer
// so header and records can never drift apart.
func csvHeader() []string {
	return []string{
		"task_id", "tier", "task_name", "provider", "strategy",
		"build_ok", "tests_present", "test_ok",
		"hidden_present", "hidden_build_ok", "hidden_test_ok",
		"reviewer_score", "approved", "false_approvals", "revisions_used",
		"duration_ms", "input_tokens", "output_tokens",
		"llm_calls", "cost_usd", "error",
	}
}

// rowStreamer appends each result row to results.csv the moment it completes and
// fsyncs, so a crash (or an OOM-kill, which the README documents happening on
// local models) keeps every row finished so far instead of discarding the whole
// run. The full JSON/Markdown reports are still rendered once at the end.
type rowStreamer struct {
	f *os.File
	w *csv.Writer
}

func newRowStreamer(path string) (*rowStreamer, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	w := csv.NewWriter(f)
	if err := w.Write(csvHeader()); err != nil {
		_ = f.Close()
		return nil, err
	}
	w.Flush()
	_ = f.Sync()
	return &rowStreamer{f: f, w: w}, nil
}

func (s *rowStreamer) add(r Row) {
	_ = s.w.Write(csvRecord(r))
	s.w.Flush()
	_ = s.f.Sync() // durability: survive a crash on the very next run
}

func (s *rowStreamer) close() {
	s.w.Flush()
	_ = s.f.Close()
}

// csvRecord renders a single Row as a CSV record. Column order must match
// csvHeader().
func csvRecord(r Row) []string {
	return []string{
		r.TaskID, r.Tier, r.TaskName, r.Provider, r.Strategy,
		strconv.FormatBool(r.BuildOK),
		strconv.FormatBool(r.TestsPresent),
		strconv.FormatBool(r.TestOK),
		strconv.FormatBool(r.HiddenPresent),
		strconv.FormatBool(r.HiddenBuildOK),
		strconv.FormatBool(r.HiddenTestOK),
		strconv.Itoa(r.ReviewerScore),
		strconv.FormatBool(r.Approved),
		strconv.Itoa(r.FalseApproval),
		strconv.Itoa(r.RevisionsUsed),
		strconv.FormatInt(r.DurationMs, 10),
		strconv.Itoa(r.InputTokens),
		strconv.Itoa(r.OutputTokens),
		strconv.Itoa(r.LLMCalls),
		strconv.FormatFloat(r.CostUSD, 'f', 6, 64),
		r.Error,
	}
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

// runMeta is the provenance stamp written alongside each results set. It records
// exactly what code and which models produced the numbers, so a committed result
// is reproducible rather than an anonymous table.
type runMeta struct {
	GeneratedAt   string            `json:"generated_at"`
	GitSHA        string            `json:"git_sha"`
	GitDirty      bool              `json:"git_dirty"`
	GoVersion     string            `json:"go_version"`
	OS            string            `json:"os"`
	Arch          string            `json:"arch"`
	MaxRevisions  int               `json:"max_revisions"`
	Strategies    []string          `json:"strategies"`
	TasksGlob     string            `json:"tasks_glob"`
	TaskIDs       []string          `json:"task_ids"`
	Providers     []string          `json:"providers"`
	ProviderNames map[string]string `json:"provider_model_names"` // cli name → Provider.Name()
	TotalRuns     int               `json:"total_runs"`
	ElapsedSec    float64           `json:"elapsed_seconds"`
}

func writeRunJSON(path string, m runMeta) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(m)
}

// gitState returns the current commit SHA and whether the working tree is dirty.
// On any failure (not a git repo, git not installed) it returns ("unknown", false)
// rather than aborting the run — provenance is best-effort, not a hard dependency.
func gitState() (sha string, dirty bool) {
	out, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "unknown", false
	}
	sha = strings.TrimSpace(string(out))
	status, err := exec.Command("git", "status", "--porcelain").Output()
	if err != nil {
		return sha, false
	}
	return sha, strings.TrimSpace(string(status)) != ""
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
	pairs := providerStrategyPairs(rows)
	fmt.Fprintln(f, "| Provider | Strategy | Build pass | Test pass (self) | Hidden pass | Avg score | Avg revisions | False approvals | Avg latency | Total cost |")
	fmt.Fprintln(f, "|---|---|---:|---:|---:|---:|---:|---:|---:|---:|")
	for _, ps := range pairs {
		s := summarise(rows, ps.provider, ps.strategy)
		fmt.Fprintf(f, "| `%s` | %s | %d/%d | %d/%d | %s | %.1f | %.1f | %d | %s | $%.4f |\n",
			ps.provider, ps.strategy,
			s.buildPass, s.total,
			s.testPass, s.total,
			hiddenCell(s),
			s.avgScore, s.avgRevisions,
			s.falseApprovals,
			fmtMs(s.avgDurationMs),
			s.totalCostUSD,
		)
	}
	fmt.Fprintln(f)
	fmt.Fprintln(f, "**Strategy** = `pipeline` (Coordinator→Planner→Coder→Reviewer with a revision loop) or "+
		"`single` (one Coder call, no plan/review/revision). Running both is the ablation for whether the extra "+
		"agents actually beat one well-prompted call — same provider, same tasks, same objective scoring. ")
	fmt.Fprintln(f, "**Build pass** = `go build ./...` succeeded on the final artifact. ")
	fmt.Fprintln(f, "**Test pass (self)** = `go test ./...` passed AND the artifact shipped a `*_test.go` file — i.e. the model's *own* tests passed. ")
	fmt.Fprintln(f, "**Hidden pass** = the implementation passed a held-out, harness-authored acceptance test the coder never saw "+
		"(over the tasks that have one). This is the metric that cannot be gamed: where **Hidden pass < Test pass (self)**, the "+
		"model wrote tests too weak to catch its own bugs. `—` means no task in that row carried a hidden test. ")
	fmt.Fprintln(f, "**Avg score** = mean of the reviewer's final 0–10 score (single-call rows have no reviewer, shown as 0.0). ")
	fmt.Fprintln(f, "**Avg revisions** = mean coder→reviewer cycles used (0 = approved first try; max = revision cap hit; always 0 for single). ")
	fmt.Fprintln(f, "**False approvals** = times the reviewer approved an artifact whose `go build`/`go test` was still red, "+
		"forcing the orchestrator's objective gate to override the approval and keep revising. A non-zero count means the "+
		"LLM reviewer cannot be trusted as the sole quality gate — the whole reason build/test is scored independently.")
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
		fmt.Fprintln(f, "| Task | Provider | Strategy | Build | Test | Hidden | Score | Revs | Latency | Cost |")
		fmt.Fprintln(f, "|---|---|---|:---:|:---:|:---:|---:|---:|---:|---:|")
		// Sort: by task id, then provider (mock first), then strategy (pipeline first)
		sort.SliceStable(tierRows, func(i, j int) bool {
			if tierRows[i].TaskID != tierRows[j].TaskID {
				return tierRows[i].TaskID < tierRows[j].TaskID
			}
			if tierRows[i].Provider != tierRows[j].Provider {
				if tierRows[i].Provider == "mock" {
					return true
				}
				if tierRows[j].Provider == "mock" {
					return false
				}
				return tierRows[i].Provider < tierRows[j].Provider
			}
			return tierRows[i].Strategy < tierRows[j].Strategy // "pipeline" < "single"
		})
		for _, r := range tierRows {
			fmt.Fprintf(f, "| %s | `%s` | %s | %s | %s | %s | %s | %d | %s | $%.4f |\n",
				r.TaskName, r.Provider, r.Strategy,
				tickFor(r.BuildOK, r.Error != ""),
				tickForTest(r),
				tickForHidden(r),
				scoreCell(r),
				r.RevisionsUsed,
				fmtMs(float64(r.DurationMs)),
				r.CostUSD,
			)
		}
		fmt.Fprintln(f)
	}

	writeFailureAnalysis(f, rows)

	fmt.Fprintln(f, "Raw data: [`results.csv`](results.csv) · [`summary.json`](summary.json)")
	return nil
}

// writeFailureAnalysis emits a per-failure breakdown grounded in each artifact's
// actual go build / go test output, so the report reads like an eval rather than
// a leaderboard. Mock is excluded: it is the all-green harness contract test, not
// a model under evaluation. A failure is any non-mock row that errored before
// producing an artifact, failed to build, or built but failed its tests.
func writeFailureAnalysis(f io.Writer, rows []Row) {
	var failures []Row
	for _, r := range rows {
		if r.Provider == "mock" {
			continue
		}
		if r.Error != "" || !r.BuildOK || (r.TestsPresent && !r.TestOK) ||
			(r.HiddenPresent && r.BuildOK && !r.HiddenTestOK) {
			failures = append(failures, r)
		}
	}

	fmt.Fprintln(f, "## Failure analysis")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "Each failure below is grounded in the artifact's real `go build` / `go test` output. "+
		"Mock is the all-green harness baseline and is excluded.")
	fmt.Fprintln(f)

	if len(failures) == 0 {
		fmt.Fprintln(f, "No build or test failures across the non-baseline providers in this run.")
		fmt.Fprintln(f)
		return
	}

	sort.SliceStable(failures, func(i, j int) bool {
		if failures[i].Provider != failures[j].Provider {
			return failures[i].Provider < failures[j].Provider
		}
		if failures[i].Strategy != failures[j].Strategy {
			return failures[i].Strategy < failures[j].Strategy
		}
		return failures[i].TaskID < failures[j].TaskID
	})

	for _, r := range failures {
		stage, evidence := classifyFailure(r)
		fmt.Fprintf(f, "### `%s` (%s) — %s\n\n", r.Provider, r.Strategy, r.TaskName)
		fmt.Fprintf(f, "- **Stage:** %s\n", stage)
		fmt.Fprintf(f, "- **Revisions used:** %d", r.RevisionsUsed)
		if r.ReviewerScore >= 0 {
			fmt.Fprintf(f, " · **reviewer score:** %d/10 (approved=%v)", r.ReviewerScore, r.Approved)
		}
		fmt.Fprintln(f)
		if evidence != "" {
			fmt.Fprintf(f, "- **Evidence (captured output):**\n\n```\n%s\n```\n", evidence)
		}
		fmt.Fprintln(f)
	}
}

// classifyFailure maps a failing row to the stage it failed at and the captured
// output that evidences it. The order matters: an error means no artifact was
// produced; otherwise build is checked before test.
func classifyFailure(r Row) (stage, evidence string) {
	switch {
	case r.Error != "":
		return "errored before a buildable artifact — the pipeline could not parse the model's output",
			truncate(strings.TrimSpace(r.Error), 600)
	case !r.BuildOK:
		return "`go build` failed — the model emitted uncompilable Go",
			truncate(strings.TrimSpace(r.BuildOut), 600)
	case r.TestsPresent && !r.TestOK:
		return "built, but `go test` failed",
			truncate(strings.TrimSpace(r.TestOut), 600)
	case r.HiddenPresent && !r.HiddenBuildOK:
		return "passed its own tests, but the held-out acceptance test does not compile against it " +
				"(wrong signature or missing export — the implementation doesn't meet the spec'd contract)",
			truncate(strings.TrimSpace(r.HiddenOut), 600)
	case r.HiddenPresent && !r.HiddenTestOK:
		return "passed its own tests, but FAILED the held-out acceptance test — the model's self-authored " +
				"tests were too weak to catch a bug the hidden test exercises (self-grading gap)",
			truncate(strings.TrimSpace(r.HiddenOut), 600)
	default:
		return "unknown", ""
	}
}

// --- aggregation helpers --------------------------------------------------

type summary struct {
	total                                 int
	buildPass, testPass                   int
	hiddenPresent, hiddenPass             int
	falseApprovals                        int
	avgScore, avgRevisions, avgDurationMs float64
	totalCostUSD                          float64
}

func summarise(rows []Row, provider, strategy string) summary {
	var s summary
	var scoreSum, scoreN int
	for _, r := range rows {
		if r.Provider != provider || r.Strategy != strategy {
			continue
		}
		s.total++
		if r.BuildOK {
			s.buildPass++
		}
		if r.TestOK && r.TestsPresent {
			s.testPass++
		}
		if r.HiddenPresent {
			s.hiddenPresent++
			if r.HiddenTestOK {
				s.hiddenPass++
			}
		}
		s.falseApprovals += r.FalseApproval
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

// provStrat is one (provider, strategy) cell of the result matrix.
type provStrat struct{ provider, strategy string }

// providerStrategyPairs returns the (provider, strategy) combinations present in
// the rows, ordered for stable reports: mock first, then providers
// alphabetically, and within each provider the pipeline strategy before single
// so the ablation reads pipeline-then-baseline.
func providerStrategyPairs(rows []Row) []provStrat {
	seen := map[provStrat]bool{}
	var out []provStrat
	for _, r := range rows {
		ps := provStrat{r.Provider, r.Strategy}
		if !seen[ps] {
			seen[ps] = true
			out = append(out, ps)
		}
	}
	stratRank := func(s string) int {
		if s == "single" {
			return 1
		}
		return 0 // pipeline (or anything else) first
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].provider != out[j].provider {
			if out[i].provider == "mock" {
				return true
			}
			if out[j].provider == "mock" {
				return false
			}
			return out[i].provider < out[j].provider
		}
		return stratRank(out[i].strategy) < stratRank(out[j].strategy)
	})
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

// hiddenCell renders the held-out pass rate for a rollup group, or "—" when no
// task in the group carried a hidden test.
func hiddenCell(s summary) string {
	if s.hiddenPresent == 0 {
		return "—"
	}
	return fmt.Sprintf("%d/%d", s.hiddenPass, s.hiddenPresent)
}

// tickForHidden renders a single row's held-out result: ✓/✗, or "—" when the
// task has no hidden test.
func tickForHidden(r Row) string {
	if !r.HiddenPresent {
		return "—"
	}
	if r.HiddenTestOK {
		return "✓"
	}
	return "✗"
}

func fmtMs(ms float64) string {
	if ms < 1000 {
		return fmt.Sprintf("%.0fms", ms)
	}
	return fmt.Sprintf("%.1fs", ms/1000)
}

// --- small utilities ------------------------------------------------------

// dumpArtifact writes the model's generated files verbatim under
// <artifactsDir>/<provider>/<taskID>/ so the raw output can be inspected.
// The content written here is exactly what runner.Run writes before building,
// so it is the literal model output, not a harness transformation.
func dumpArtifact(artifactsDir, provider, taskID string, a *models.Artifact) error {
	safeProvider := strings.NewReplacer("/", "_", ":", "-").Replace(provider)
	base := filepath.Join(artifactsDir, safeProvider, taskID)
	if err := os.MkdirAll(base, 0o755); err != nil {
		return err
	}
	for _, f := range a.Files {
		dest := filepath.Join(base, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dest, []byte(f.Content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// tmpRunPathRe matches the absolute path of a runner temp dir (Windows or
// Unix) that the Go toolchain echoes into build/test output. We strip the
// leading directory so committed results don't leak the local username/home.
var tmpRunPathRe = regexp.MustCompile(`(?:[A-Za-z]:\\[^\s"]*?|/[^\s"]*?)mugi-run-\d+`)

// scrubPaths replaces machine-specific temp paths with a stable placeholder so
// benchmark output is reproducible and free of local identifiers.
func scrubPaths(s string) string {
	return tmpRunPathRe.ReplaceAllString(s, "<tmpdir>/mugi-run-XXXX")
}

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
