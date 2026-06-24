// Command swebench-predict turns SWE-bench instances into a predictions file the
// official harness can score. For each instance it checks out the repo at its
// base commit, retrieves the most relevant code, asks the model for a single
// unified diff, and writes a `{instance_id, model_name_or_path, model_patch}` row.
//
// The model is chosen by LLM_PROVIDER (default: mock — no spend). Only an explicit
// real provider costs tokens:
//
//	# offline dry run — mock model, $0 (proves the pipeline):
//	go run ./cmd/swebench-predict -instances slice.jsonl -mode lexical -k 10
//
//	# real run — this SPENDS on the chosen API:
//	LLM_PROVIDER=anthropic ANTHROPIC_API_KEY=... \
//	  go run ./cmd/swebench-predict -instances slice.jsonl -mode lexical -k 10
//
// It clones public repositories; run it inside CI or a sandbox, not on a personal
// machine. The slice file is a SWE-bench dataset export (JSONL or JSON array).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"mugi/internal/embedenv"
	"mugi/internal/index"
	"mugi/internal/llm"
	"mugi/internal/predict"
	"mugi/internal/retrievaleval"
	"mugi/internal/swebench"
)

func main() {
	instances := flag.String("instances", "", "path to a SWE-bench slice (JSONL or JSON array) (required)")
	mode := flag.String("mode", "lexical", "retrieval mode: lexical | semantic | hybrid")
	k := flag.Int("k", 10, "number of chunks to retrieve into the prompt")
	window := flag.Int("window", 50, "chunk size in lines")
	overlap := flag.Int("overlap", 10, "overlap between chunks in lines")
	limit := flag.Int("limit", 0, "predict for at most this many instances (0 = all)")
	workdir := flag.String("workdir", "", "parent directory for temporary checkouts (default: OS temp)")
	gitTimeout := flag.Duration("git-timeout", 5*time.Minute, "timeout per git command")
	maxTokens := flag.Int("max-tokens", 4096, "max tokens for the model's diff output")
	temperature := flag.Float64("temperature", 0, "sampling temperature")
	out := flag.String("out", "predictions.jsonl", "write the predictions file here")
	flag.Parse()

	if *instances == "" {
		fmt.Fprintln(os.Stderr, "swebench-predict: -instances is required")
		flag.Usage()
		os.Exit(2)
	}

	provider, err := llm.NewFromEnv()
	if err != nil {
		fail("provider: %v", err)
	}

	insts, err := swebench.Load(*instances)
	if err != nil {
		fail("load %s: %v", *instances, err)
	}
	if *limit > 0 && *limit < len(insts) {
		insts = insts[:*limit]
	}
	if len(insts) == 0 {
		fail("no instances to predict for")
	}

	// Build the retrieval index builder; only semantic/hybrid need an embedder.
	var emb index.Embedder
	if *mode == "semantic" || *mode == "hybrid" {
		e := embedenv.FromEnv(os.Stderr)
		defer e.Save()
		emb = e
	}
	build, err := retrievaleval.ModeBuilder(*mode, emb)
	if err != nil {
		fail("%v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	src := retrievaleval.GitRepoSource{WorkDir: *workdir, Timeout: *gitTimeout}
	cfg := retrievaleval.Config{Window: *window, Overlap: *overlap}
	gen := predict.Generator{
		Provider: provider,
		Config:   predict.Config{MaxTokens: *maxTokens, Temperature: *temperature},
	}

	spend := "no spend"
	if provider.Name() != "mock" {
		spend = "THIS WILL CALL A PAID API"
	}
	fmt.Fprintf(os.Stderr, "predicting for %d instances · model=%s (%s) · mode=%s · k=%d\n\n",
		len(insts), provider.Name(), spend, *mode, *k)

	preds := make([]predict.Prediction, 0, len(insts))
	var valid, plausible, errored int
	for _, in := range insts {
		if err := ctx.Err(); err != nil {
			fmt.Fprintln(os.Stderr, "interrupted; writing what we have")
			break
		}
		task := predict.Task{ID: in.InstanceID, Repo: in.Repo, ProblemStatement: in.ProblemStatement}

		chunks, rerr := retrieve(ctx, src, in, cfg, build, *k)
		if rerr != nil {
			errored++
			preds = append(preds, predict.Prediction{InstanceID: in.InstanceID, Model: provider.Name()})
			fmt.Fprintf(os.Stderr, "  %-30s retrieval failed: %v\n", in.InstanceID, rerr)
			continue
		}

		res := gen.Predict(ctx, task, chunks)
		preds = append(preds, res.Prediction)
		switch {
		case res.Err != "":
			errored++
			fmt.Fprintf(os.Stderr, "  %-30s no diff: %s\n", in.InstanceID, res.Err)
		default:
			valid++
			if res.PlausibleHit {
				plausible++
			}
			fmt.Fprintf(os.Stderr, "  %-30s diff touches %v (plausible=%v)\n",
				in.InstanceID, res.ChangedFiles, res.PlausibleHit)
		}
	}

	if err := predict.WritePredictions(*out, preds); err != nil {
		fail("write %s: %v", *out, err)
	}
	fmt.Fprintf(os.Stderr, "\n%d instances · %d valid diffs · %d plausible · %d errored\nwrote %s\n",
		len(preds), valid, plausible, errored, *out)
}

// retrieve checks out an instance's repo and returns the top-k chunks, always
// releasing the checkout.
func retrieve(ctx context.Context, src retrievaleval.RepoSource, in swebench.Instance, cfg retrievaleval.Config, build retrievaleval.IndexBuilder, k int) ([]index.Chunk, error) {
	dir, cleanup, err := src.Checkout(ctx, in.Repo, in.BaseCommit)
	if err != nil {
		return nil, fmt.Errorf("checkout: %w", err)
	}
	defer cleanup()
	return retrievaleval.RetrieveFrom(ctx, dir, in.ProblemStatement, cfg, build, k)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "swebench-predict: "+format+"\n", args...)
	os.Exit(1)
}
