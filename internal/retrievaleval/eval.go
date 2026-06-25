// Package retrievaleval measures how well repository retrieval localizes a fix.
//
// For a SWE-bench task it builds a retrieval index over the repository at its
// base commit, retrieves against the problem statement, and computes recall@k
// against the files the gold patch actually changed. That is the free tuning
// signal — no model generation, no token spend — used to get retrieval right
// (chunk size, k, fusion) before any money is spent on an agent.
//
// The package joins two pure cores — index retrieval and the SWE-bench dataset —
// behind a checkout seam (RepoSource), so the whole flow is unit-testable offline
// with a fixture source and the deterministic mock embedder, the same discipline
// as the swebench LocalEnv. It never executes repository content: indexing is
// read-only text processing.
package retrievaleval

import (
	"context"
	"fmt"
	"sort"

	"mugi/internal/index"
)

// Task is the minimal description of one retrieval-evaluation case: a repository
// at a commit, the text to retrieve against, and the gold patch whose changed
// files are the ground truth for recall. It is the boundary model against the
// fuller swebench.Instance — this package needs only these fields, so callers map
// an instance down to a Task and nothing here depends on the dataset schema.
type Task struct {
	ID               string
	Repo             string // "owner/name"
	BaseCommit       string // SHA to check out
	ProblemStatement string // the query
	GoldPatch        string // reference solution; its changed files are the target
}

// Config holds the retrieval knobs being tuned. Zero values fall back to sensible
// defaults, and a zero MaxFileBytes/MaxChunks defers to the chunker's own caps.
type Config struct {
	K             int   // chunks to retrieve (single-shot default; sweeps pass explicit ks)
	Window        int   // chunk size in lines
	Overlap       int   // overlap between adjacent chunks in lines
	MaxFileBytes  int64 // skip files larger than this (0 = chunker default)
	MaxChunks     int   // cap chunks per repo (0 = chunker default)
	MaxQueryRunes int   // cap query length before retrieval/embedding (a safeguard)
}

func (c Config) withDefaults() Config {
	if c.K <= 0 {
		c.K = 10
	}
	if c.Window <= 0 {
		c.Window = 50
	}
	if c.Overlap < 0 {
		c.Overlap = 10
	}
	if c.MaxQueryRunes <= 0 {
		c.MaxQueryRunes = 4000
	}
	return c
}

// IndexBuilder builds a queryable index over chunks. Injecting it decouples the
// measurement from how an index is constructed: lexical needs nothing, semantic
// and hybrid need an embedder. The context governs any embedding call inside.
type IndexBuilder func(ctx context.Context, chunks []index.Chunk) (index.Index, error)

// ModeSpec pairs a human-readable mode label with its builder, so a sweep can
// report results per mode.
type ModeSpec struct {
	Label string
	Build IndexBuilder
}

// ModeBuilder returns the IndexBuilder for a retrieval mode. lexical is fully
// offline; semantic and hybrid require a non-nil embedder (a local Ollama keeps
// them free). An unknown mode, or a missing embedder where one is needed, is an
// error surfaced at configuration time rather than mid-run.
func ModeBuilder(mode string, emb index.Embedder) (IndexBuilder, error) {
	switch mode {
	case "lexical":
		return func(_ context.Context, chunks []index.Chunk) (index.Index, error) {
			return index.NewLexicalIndex(chunks), nil
		}, nil
	case "semantic":
		if emb == nil {
			return nil, fmt.Errorf("retrievaleval: semantic mode needs an embedder")
		}
		return func(ctx context.Context, chunks []index.Chunk) (index.Index, error) {
			return index.NewSemanticIndex(ctx, chunks, emb)
		}, nil
	case "hybrid":
		if emb == nil {
			return nil, fmt.Errorf("retrievaleval: hybrid mode needs an embedder")
		}
		return func(ctx context.Context, chunks []index.Chunk) (index.Index, error) {
			sem, err := index.NewSemanticIndex(ctx, chunks, emb)
			if err != nil {
				return nil, err
			}
			return index.NewHybridIndex(0, index.NewLexicalIndex(chunks), sem), nil
		}, nil
	default:
		return nil, fmt.Errorf("retrievaleval: unknown mode %q (want lexical|semantic|hybrid)", mode)
	}
}

// InstanceResult is one (instance, mode, k) measurement: did retrieval surface
// the files the gold patch changed?
type InstanceResult struct {
	InstanceID     string   `json:"instance_id"`
	Mode           string   `json:"mode"`
	K              int      `json:"k"`
	ChangedFiles   []string `json:"changed_files"`
	RetrievedFiles []string `json:"retrieved_files"`
	RecallAtK      float64  `json:"recall_at_k"`
	FullHit        bool     `json:"full_hit"` // every changed file was retrieved
	ChunksIndexed  int      `json:"chunks_indexed"`
	Err            string   `json:"error,omitempty"`
}

// EvaluateTask measures recall for one already-checked-out repository across the
// given modes and k values. dir must be a checkout of the task's repo; this
// function only reads it.
//
// It chunks the repo once, then per mode builds the index once and retrieves at
// the largest k, deriving every smaller k by truncation — the rankings are a
// prefix-stable order, so top-k equals the top-maxK sliced to k. A multi-mode,
// multi-k sweep therefore costs one chunking pass and one index build per mode
// (and, with the embedding cache, one embed per chunk across modes).
func EvaluateTask(ctx context.Context, dir string, t Task, cfg Config, modes []ModeSpec, ks []int) ([]InstanceResult, error) {
	cfg = cfg.withDefaults()
	if len(ks) == 0 {
		ks = []int{cfg.K}
	}
	ks = sortedUnique(ks)
	maxK := ks[len(ks)-1]

	changed := index.ChangedFiles(t.GoldPatch)
	chunks, err := chunkRepo(dir, cfg)
	if err != nil {
		return nil, err
	}
	query := capRunes(t.ProblemStatement, cfg.MaxQueryRunes)

	var out []InstanceResult
	for _, m := range modes {
		ix, err := m.Build(ctx, chunks)
		if err != nil {
			out = append(out, failedRows(t, m.Label, ks, len(chunks), changed, fmt.Sprintf("build index: %v", err))...)
			continue
		}
		hits, err := ix.Retrieve(ctx, query, maxK)
		if err != nil {
			out = append(out, failedRows(t, m.Label, ks, len(chunks), changed, fmt.Sprintf("retrieve: %v", err))...)
			continue
		}
		for _, k := range ks {
			top := hits
			if k < len(top) {
				top = top[:k]
			}
			recall := index.RecallAtK(top, changed)
			out = append(out, InstanceResult{
				InstanceID:     t.ID,
				Mode:           m.Label,
				K:              k,
				ChangedFiles:   changed,
				RetrievedFiles: distinctFiles(top),
				RecallAtK:      recall,
				FullHit:        len(changed) > 0 && recall == 1.0,
				ChunksIndexed:  len(chunks),
			})
		}
	}
	return out, nil
}

// chunkRepo turns a checked-out repo into chunks under the config's caps. It is
// the one place chunking parameters are applied, shared by the recall flow and
// the retrieval helper.
func chunkRepo(dir string, cfg Config) ([]index.Chunk, error) {
	chunks, err := index.WindowChunker{
		WindowLines:  cfg.Window,
		OverlapLines: cfg.Overlap,
		MaxFileBytes: cfg.MaxFileBytes,
		MaxChunks:    cfg.MaxChunks,
	}.Chunk(dir)
	if err != nil {
		return nil, fmt.Errorf("chunk %s: %w", dir, err)
	}
	return chunks, nil
}

// RetrieveFrom chunks an already-checked-out repo, builds an index with build,
// and returns the top-k chunks for query. It is the retrieval half of the recall
// flow exposed for callers — like prediction — that need the chunks themselves
// rather than a recall score. dir is only read.
func RetrieveFrom(ctx context.Context, dir, query string, cfg Config, build IndexBuilder, k int) ([]index.Chunk, error) {
	cfg = cfg.withDefaults()
	chunks, err := chunkRepo(dir, cfg)
	if err != nil {
		return nil, err
	}
	ix, err := build(ctx, chunks)
	if err != nil {
		return nil, fmt.Errorf("build index: %w", err)
	}
	hits, err := ix.Retrieve(ctx, capRunes(query, cfg.MaxQueryRunes), k)
	if err != nil {
		return nil, fmt.Errorf("retrieve: %w", err)
	}
	return hits, nil
}

// OracleChunks returns chunks drawn only from the given files — the changed files
// of an instance's gold patch — capped to k. It is the retrieval upper bound for
// the ablation: the model is shown exactly the files the fix touches (their actual
// contents), without ever being shown the fix itself, so any failure from here on
// is the generator's, not retrieval's. files come from index.ChangedFiles(patch);
// dir is only read.
func OracleChunks(dir string, files []string, cfg Config, k int) ([]index.Chunk, error) {
	cfg = cfg.withDefaults()
	chunks, err := chunkRepo(dir, cfg)
	if err != nil {
		return nil, err
	}
	want := make(map[string]bool, len(files))
	for _, f := range files {
		want[f] = true
	}
	out := make([]index.Chunk, 0, k)
	for _, c := range chunks {
		if !want[c.Path] {
			continue
		}
		out = append(out, c)
		if k > 0 && len(out) >= k {
			break
		}
	}
	return out, nil
}

// Run evaluates every task over the given source: for each it checks out the
// repo, runs EvaluateTask, and releases the checkout. A checkout or eval failure
// is recorded per (mode, k) row rather than aborting the run, so one bad repo
// does not sink the slice. The context cancels in-flight git and embedding calls.
func Run(ctx context.Context, src RepoSource, tasks []Task, cfg Config, modes []ModeSpec, ks []int) []InstanceResult {
	cfg = cfg.withDefaults()
	if len(ks) == 0 {
		ks = []int{cfg.K}
	}
	ks = sortedUnique(ks)

	var all []InstanceResult
	for _, t := range tasks {
		if err := ctx.Err(); err != nil {
			break
		}
		dir, cleanup, err := src.Checkout(ctx, t.Repo, t.BaseCommit)
		if err != nil {
			all = append(all, failedTaskRows(t, modes, ks, fmt.Sprintf("checkout: %v", err))...)
			continue
		}
		rows, err := EvaluateTask(ctx, dir, t, cfg, modes, ks)
		cleanup()
		if err != nil {
			all = append(all, failedTaskRows(t, modes, ks, err.Error())...)
			continue
		}
		all = append(all, rows...)
	}
	return all
}

// failedRows builds one error result per k for a single mode.
func failedRows(t Task, mode string, ks []int, chunks int, changed []string, msg string) []InstanceResult {
	out := make([]InstanceResult, 0, len(ks))
	for _, k := range ks {
		out = append(out, InstanceResult{
			InstanceID:    t.ID,
			Mode:          mode,
			K:             k,
			ChangedFiles:  changed,
			ChunksIndexed: chunks,
			Err:           msg,
		})
	}
	return out
}

// failedTaskRows builds one error result per (mode, k) when a task fails before
// any mode could run (e.g. checkout or chunking failed).
func failedTaskRows(t Task, modes []ModeSpec, ks []int, msg string) []InstanceResult {
	changed := index.ChangedFiles(t.GoldPatch)
	var out []InstanceResult
	for _, m := range modes {
		out = append(out, failedRows(t, m.Label, ks, 0, changed, msg)...)
	}
	return out
}

func distinctFiles(chunks []index.Chunk) []string {
	seen := make(map[string]bool, len(chunks))
	var out []string
	for _, c := range chunks {
		if !seen[c.Path] {
			seen[c.Path] = true
			out = append(out, c.Path)
		}
	}
	return out
}

func sortedUnique(ks []int) []int {
	seen := make(map[int]bool, len(ks))
	out := make([]int, 0, len(ks))
	for _, k := range ks {
		if k > 0 && !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	sort.Ints(out)
	if len(out) == 0 {
		out = []int{10}
	}
	return out
}

// capRunes truncates s to at most max runes (max <= 0 means no cap). A very long
// problem statement is bounded before it reaches an embedder with a token limit.
func capRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
