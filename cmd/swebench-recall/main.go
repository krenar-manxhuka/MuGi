// Command swebench-recall measures retrieval recall@k on SWE-bench instances —
// the free tuning step before any model spend. For each instance it checks out
// the repo at its base commit, indexes it, retrieves against the problem
// statement, and scores how often the gold patch's changed files are surfaced.
//
//	# lexical — no embedder, no key, $0:
//	go run ./cmd/swebench-recall -instances slice.jsonl -modes lexical -k 5,10,20
//
//	# hybrid — point EMBED_BASE_URL at a local Ollama for free embeddings:
//	EMBED_BASE_URL=http://localhost:11434/v1 EMBED_MODEL=nomic-embed-text \
//	  go run ./cmd/swebench-recall -instances slice.jsonl -modes lexical,hybrid -k 10
//
// It clones public repositories; run it inside CI or a sandbox, not on a personal
// machine. The slice file is a SWE-bench dataset export (JSONL or JSON array).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"mugi/internal/embedenv"
	"mugi/internal/index"
	"mugi/internal/retrievaleval"
	"mugi/internal/swebench"
)

func main() {
	instances := flag.String("instances", "", "path to a SWE-bench slice (JSONL or JSON array) (required)")
	modesArg := flag.String("modes", "lexical", "comma-separated retrieval modes: lexical,semantic,hybrid")
	ksArg := flag.String("k", "5,10,20", "comma-separated k values to score recall at")
	window := flag.Int("window", 50, "chunk size in lines")
	overlap := flag.Int("overlap", 10, "overlap between chunks in lines")
	limit := flag.Int("limit", 0, "evaluate at most this many instances (0 = all)")
	workdir := flag.String("workdir", "", "parent directory for temporary checkouts (default: OS temp)")
	timeout := flag.Duration("git-timeout", 5*time.Minute, "timeout per git command")
	out := flag.String("out", "recall.json", "write the full results JSON here")
	flag.Parse()

	if strings.TrimSpace(*instances) == "" {
		fmt.Fprintln(os.Stderr, "swebench-recall: -instances is required")
		flag.Usage()
		os.Exit(2)
	}

	ks, err := parseInts(*ksArg)
	if err != nil {
		fail("parse -k: %v", err)
	}
	modes := splitCSV(*modesArg)
	if len(modes) == 0 {
		fail("no -modes given")
	}

	insts, err := swebench.Load(*instances)
	if err != nil {
		fail("load %s: %v", *instances, err)
	}
	tasks := toTasks(insts)
	if *limit > 0 && *limit < len(tasks) {
		tasks = tasks[:*limit]
	}
	if len(tasks) == 0 {
		fail("no instances to evaluate")
	}

	// Only stand up an embedder if a mode actually needs one — lexical stays $0.
	var emb index.Embedder
	if needsEmbedder(modes) {
		e := embedenv.FromEnv(os.Stderr)
		defer e.Save()
		emb = e
	}
	specs, err := buildSpecs(modes, emb)
	if err != nil {
		fail("%v", err)
	}

	// Cancel in-flight git and embedding calls on Ctrl-C instead of leaving them.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	src := retrievaleval.GitRepoSource{WorkDir: *workdir, Timeout: *timeout}
	cfg := retrievaleval.Config{Window: *window, Overlap: *overlap}

	fmt.Fprintf(os.Stderr, "evaluating %d instances · modes=%s · k=%s\n\n",
		len(tasks), strings.Join(modes, ","), *ksArg)
	results := retrievaleval.Run(ctx, src, tasks, cfg, specs, ks)
	aggs := retrievaleval.Summarize(results)

	fmt.Fprint(os.Stderr, retrievaleval.FormatTable(aggs))

	if err := writeReport(*out, modes, ks, *window, *overlap, embedderName(emb), len(tasks), results, aggs); err != nil {
		fail("write %s: %v", *out, err)
	}
	fmt.Fprintf(os.Stderr, "\nwrote %s\n", *out)
}

func toTasks(insts []swebench.Instance) []retrievaleval.Task {
	tasks := make([]retrievaleval.Task, 0, len(insts))
	for _, in := range insts {
		tasks = append(tasks, retrievaleval.Task{
			ID:               in.InstanceID,
			Repo:             in.Repo,
			BaseCommit:       in.BaseCommit,
			ProblemStatement: in.ProblemStatement,
			GoldPatch:        in.Patch,
		})
	}
	return tasks
}

func buildSpecs(modes []string, emb index.Embedder) ([]retrievaleval.ModeSpec, error) {
	specs := make([]retrievaleval.ModeSpec, 0, len(modes))
	for _, m := range modes {
		b, err := retrievaleval.ModeBuilder(m, emb)
		if err != nil {
			return nil, err
		}
		specs = append(specs, retrievaleval.ModeSpec{Label: m, Build: b})
	}
	return specs, nil
}

func needsEmbedder(modes []string) bool {
	for _, m := range modes {
		if m == "semantic" || m == "hybrid" {
			return true
		}
	}
	return false
}

func embedderName(emb index.Embedder) string {
	if emb == nil {
		return "none"
	}
	return emb.Name()
}

// report is the persisted run record: enough provenance to reproduce and compare.
type report struct {
	GeneratedAt time.Time                      `json:"generated_at"`
	Modes       []string                       `json:"modes"`
	Ks          []int                          `json:"k"`
	Window      int                            `json:"window"`
	Overlap     int                            `json:"overlap"`
	Embedder    string                         `json:"embedder"`
	Instances   int                            `json:"instances"`
	Aggregates  []retrievaleval.Aggregate      `json:"aggregates"`
	Results     []retrievaleval.InstanceResult `json:"results"`
}

func writeReport(path string, modes []string, ks []int, window, overlap int, embedder string, instances int, results []retrievaleval.InstanceResult, aggs []retrievaleval.Aggregate) error {
	rep := report{
		GeneratedAt: time.Now().UTC(),
		Modes:       modes,
		Ks:          ks,
		Window:      window,
		Overlap:     overlap,
		Embedder:    embedder,
		Instances:   instances,
		Aggregates:  aggs,
		Results:     results,
	}
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func parseInts(csv string) ([]int, error) {
	parts := splitCSV(csv)
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("%q is not an integer", p)
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no values")
	}
	return out, nil
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "swebench-recall: "+format+"\n", args...)
	os.Exit(1)
}
