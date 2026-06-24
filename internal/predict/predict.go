// Package predict turns a SWE-bench task plus the code retrieval surfaced into a
// prediction: a single unified diff the official harness can apply and score.
//
// It is a single well-prompted model call by design — the bench ablation found a
// 4-agent pipeline does not beat one good call for a strong model — wrapped in a
// strict output contract: the model is asked for a diff and nothing else, and the
// reply is extracted and sanity-checked before it becomes a prediction. The whole
// package is offline-testable through the llm.Provider seam with the mock
// provider; only a real provider spends tokens.
package predict

import (
	"context"
	"fmt"

	"mugi/internal/index"
	"mugi/internal/llm"
	"mugi/internal/prompts"
)

// Task is the minimal input to generate a prediction. It deliberately omits the
// gold patch: generation must never see the answer it is being measured against.
type Task struct {
	ID               string
	Repo             string // "owner/name", for orienting the model
	ProblemStatement string // the issue text — the prompt
}

// Prediction is one row of a SWE-bench predictions file. The JSON tags match what
// the official harness reads (`instance_id`, `model_name_or_path`, `model_patch`).
type Prediction struct {
	InstanceID string `json:"instance_id"`
	Model      string `json:"model_name_or_path"`
	Patch      string `json:"model_patch"`
}

// Result is a Prediction plus the provenance to judge it without re-running: which
// files the diff touches, which files retrieval had shown, whether the diff lands
// on at least one retrieved file (a cheap plausibility signal), and the raw model
// output. Err is set when no valid diff could be produced; the Prediction is then
// still returned with an empty patch, so the instance counts as attempted-but-
// unresolved rather than vanishing from the run.
type Result struct {
	Prediction     Prediction
	ChangedFiles   []string
	RetrievedFiles []string
	PlausibleHit   bool
	Raw            string
	Err            string
}

// Config holds the generation knobs. Zero values fall back to defaults tuned for
// diffs (short, low-temperature output), not whole projects.
type Config struct {
	MaxTokens        int
	Temperature      float64
	MaxContextChunks int    // most retrieved chunks to put in the prompt
	MaxChunkRunes    int    // truncate each chunk to this many runes
	ModelName        string // overrides Provider.Name() in the prediction row
}

func (c Config) withDefaults() Config {
	if c.MaxTokens <= 0 {
		c.MaxTokens = 4096
	}
	if c.Temperature < 0 {
		c.Temperature = 0
	}
	if c.MaxContextChunks <= 0 {
		c.MaxContextChunks = 20
	}
	if c.MaxChunkRunes <= 0 {
		c.MaxChunkRunes = 4000
	}
	return c
}

// Generator produces predictions through a provider. Loader may be nil, in which
// case the embedded prompt templates are used.
type Generator struct {
	Provider llm.Provider
	Loader   *prompts.Loader
	Config   Config
}

// Predict runs one task: it assembles a prompt from the problem statement and the
// retrieved chunks, asks the provider for a diff, extracts and validates it, and
// returns a Result. A provider error or an unusable reply is reported in
// Result.Err with an empty-patch Prediction — never as a process-stopping error,
// so a slice run records the miss and moves on.
func (g Generator) Predict(ctx context.Context, task Task, chunks []index.Chunk) Result {
	cfg := g.Config.withDefaults()
	model := cfg.ModelName
	if model == "" {
		model = g.Provider.Name()
	}
	res := Result{
		Prediction:     Prediction{InstanceID: task.ID, Model: model},
		RetrievedFiles: distinctFiles(chunks),
	}

	loader := g.Loader
	if loader == nil {
		loader = prompts.NewLoader("")
	}
	system, err := loader.Render("swebench_diff", systemData{Repo: task.Repo})
	if err != nil {
		res.Err = fmt.Sprintf("render prompt: %v", err)
		return res
	}

	resp, err := g.Provider.Generate(ctx, llm.Request{
		SystemPrompt: system,
		Messages:     []llm.Message{{Role: "user", Content: userMessage(task, chunks, cfg)}},
		MaxTokens:    cfg.MaxTokens,
		Temperature:  cfg.Temperature,
	})
	if err != nil {
		res.Err = fmt.Sprintf("generate: %v", err)
		return res
	}
	res.Raw = resp.Content

	diff, err := ExtractDiff(resp.Content)
	if err != nil {
		if resp.Truncated() {
			res.Err = fmt.Sprintf("%v (output hit the MaxTokens ceiling)", err)
		} else {
			res.Err = err.Error()
		}
		return res
	}

	res.Prediction.Patch = diff
	res.ChangedFiles = index.ChangedFiles(diff)
	res.PlausibleHit = overlaps(res.ChangedFiles, res.RetrievedFiles)
	return res
}

// overlaps reports whether any changed file was among the retrieved files — a
// cheap signal that the diff lands where retrieval pointed, not somewhere unseen.
func overlaps(changed, retrieved []string) bool {
	set := make(map[string]bool, len(retrieved))
	for _, f := range retrieved {
		set[f] = true
	}
	for _, f := range changed {
		if set[f] {
			return true
		}
	}
	return false
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

// capRunes truncates s to at most max runes (max <= 0 means no cap).
func capRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "\n… (truncated)"
}
