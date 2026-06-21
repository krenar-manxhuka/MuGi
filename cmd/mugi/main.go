// Command mugi runs the multi-agent software-building pipeline.
//
// Usage:
//
//	mugi "Build a REST API in Go with CRUD endpoints"
//	mugi --task "Build a REST API in Go with CRUD endpoints"
//	echo "Build a CLI tool" | mugi
//
// Configuration is via environment variables (see internal/config and .env.example).
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mugi/internal/agents"
	"mugi/internal/config"
	"mugi/internal/fsafe"
	"mugi/internal/llm"
	"mugi/internal/models"
	"mugi/internal/orchestrator"
	"mugi/internal/prompts"
	"mugi/internal/state"
)

func main() {
	loadDotEnv(".env")

	taskFlag := flag.String("task", "", "task description to execute")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	task := resolveTask(*taskFlag)
	if task == "" {
		fmt.Fprintln(os.Stderr, "error: provide a task via positional arg, --task flag, or stdin")
		fmt.Fprintln(os.Stderr, `  example: mugi "Build a simple Go HTTP server"`)
		os.Exit(1)
	}

	cfg := config.Load()

	provider, err := llm.NewFromEnv()
	if err != nil {
		slog.Error("failed to initialise LLM provider", "err", err)
		os.Exit(1)
	}
	// Cap total LLM spend per run as a defence-in-depth guardrail.
	if cfg.MaxLLMCalls > 0 {
		provider = llm.NewBudgetProvider(provider, cfg.MaxLLMCalls)
	}
	slog.Info("using LLM provider", "provider", provider.Name(), "max_llm_calls", cfg.MaxLLMCalls)

	loader := prompts.NewLoader(cfg.PromptsDir)

	coord := agents.NewCoordinator(provider, loader)
	plan := agents.NewPlanner(provider, loader)
	code := agents.NewCoder(provider, loader)
	rev := agents.NewReviewer(provider, loader)

	orch := orchestrator.New(coord, plan, code, rev, orchestrator.Config{
		MaxRevisions: cfg.MaxRevisions,
		SkipReview:   cfg.SkipReview,
		RunTests:     cfg.RunTests,
		Logger:       logger,
	})

	t := &models.Task{
		ID:          fmt.Sprintf("task-%d", time.Now().UnixMilli()),
		Description: task,
		CreatedAt:   time.Now(),
	}

	ctx := context.Background()
	st, err := orch.Run(ctx, t)
	if err != nil {
		slog.Error("workflow failed", "err", err)
		printState(st)
		os.Exit(1)
	}

	printState(st)

	if artifact := st.GetArtifact(); artifact != nil {
		if writeErr := writeArtifact(cfg.OutputDir, artifact); writeErr != nil {
			slog.Warn("could not write output files", "err", writeErr)
		} else {
			slog.Info("artifact written", "dir", cfg.OutputDir)
			printRunInstructions(cfg.OutputDir, artifact, st.GetExecResult())
		}
	}
}

// resolveTask picks up the task from (in order): --task flag, first positional
// arg, or stdin (when piped).
func resolveTask(flagVal string) string {
	if flagVal != "" {
		return strings.TrimSpace(flagVal)
	}
	if flag.NArg() > 0 {
		return strings.TrimSpace(strings.Join(flag.Args(), " "))
	}
	// Try stdin if it looks like it's piped
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		scanner := bufio.NewScanner(os.Stdin)
		var lines []string
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		return strings.TrimSpace(strings.Join(lines, "\n"))
	}
	return ""
}

// printState renders the final workflow state to stdout.
func printState(st *state.WorkflowState) {
	fmt.Println("\n" + strings.Repeat("─", 60))
	fmt.Printf("  MUGI  ·  %s\n", st.Task.Description)
	fmt.Println(strings.Repeat("─", 60))

	fmt.Printf("Status:    %s\n", st.GetStatus())
	iter, maxIter, _ := st.Snapshot()
	fmt.Printf("Revisions: %d / %d\n", iter, maxIter)

	if plan := st.GetPlan(); plan != nil {
		fmt.Printf("\nPlan: %s\n", plan.Summary)
		for _, s := range plan.Steps {
			fmt.Printf("  %d. %s\n", s.ID, s.Title)
		}
	}

	if artifact := st.GetArtifact(); artifact != nil {
		fmt.Printf("\nArtifact (revision %d): %s\n", artifact.Revision, artifact.Summary)
		for _, f := range artifact.Files {
			fmt.Printf("  %-40s  [%s]\n", f.Path, f.Lang)
		}
	}

	if reviews := st.AllReviews(); len(reviews) > 0 {
		last := reviews[len(reviews)-1]
		verdict := "needs revision"
		if last.Approved {
			verdict = "APPROVED ✓"
		}
		fmt.Printf("\nReview (revision %d): %s  score=%d/10\n", last.Revision, verdict, last.Score)
		fmt.Printf("  %s\n", last.Feedback)
		for _, issue := range last.Issues {
			fmt.Printf("  [%s] %s — %s\n", issue.Severity, issue.Description, issue.Suggestion)
		}
	}

	if log := st.GetLog(); len(log) > 0 {
		fmt.Println("\nWorkflow log:")
		for _, entry := range log {
			fmt.Printf("  %-12s %s\n", "["+entry.Agent+"]", entry.Message)
		}
	}

	fmt.Println(strings.Repeat("─", 60))
}

// writeArtifact saves the artifact files to dir, creating it if needed.
func writeArtifact(dir string, artifact *models.Artifact) error {
	// Write JSON manifest
	manifestPath := filepath.Join(dir, "manifest.json")
	if err := writeFile(manifestPath, func() ([]byte, error) {
		return json.MarshalIndent(artifact, "", "  ")
	}); err != nil {
		return err
	}

	// Write each source file, containing model-controlled paths within dir.
	for _, f := range artifact.Files {
		dest, err := fsafe.SafeJoin(dir, f.Path)
		if err != nil {
			return fmt.Errorf("unsafe artifact path %q: %w", f.Path, err)
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(dest), err)
		}
		if err := os.WriteFile(dest, []byte(f.Content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", dest, err)
		}
	}

	// Ensure the output is a buildable module so `go run .` / `go test ./...`
	// work out of the box. Mirrors the runner, which synthesises the same go.mod
	// when the model omits one — so what we ship matches what was verified.
	if !artifactHasGoMod(artifact) {
		modPath := filepath.Join(dir, "go.mod")
		if err := os.WriteFile(modPath, []byte("module generated\n\ngo 1.21\n"), 0o644); err != nil {
			return fmt.Errorf("write go.mod: %w", err)
		}
	}
	return nil
}

// artifactHasGoMod reports whether the artifact already includes a go.mod.
func artifactHasGoMod(a *models.Artifact) bool {
	for _, f := range a.Files {
		if f.Path == "go.mod" {
			return true
		}
	}
	return false
}

// isRunnable reports whether the artifact is an executable command (has a main
// package with a main function) rather than a library.
func isRunnable(a *models.Artifact) bool {
	for _, f := range a.Files {
		if strings.Contains(f.Content, "package main") && strings.Contains(f.Content, "func main(") {
			return true
		}
	}
	return false
}

// printRunInstructions tells the user exactly how to run what was produced,
// tailored to whether it's a command or a library, and flags build/test issues.
func printRunInstructions(dir string, a *models.Artifact, exec *models.ExecResult) {
	fmt.Println("\nNext steps:")
	if isRunnable(a) {
		fmt.Printf("  cd %s && go run .\n", dir)
	} else {
		fmt.Printf("  cd %s && go test ./...\n", dir)
	}
	if exec != nil && !exec.Skipped {
		switch {
		case !exec.BuildOK:
			fmt.Println("  ⚠ the generated code did not build cleanly — review it before relying on it.")
		case !exec.TestOK:
			fmt.Println("  ⚠ the code builds but some tests failed — review it before relying on it.")
		}
	}
}

func writeFile(path string, data func() ([]byte, error)) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := data()
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// loadDotEnv reads key=value pairs from a .env file and sets them as
// environment variables, skipping blank lines, comments, and any key that is
// already set in the environment.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return // no .env is fine
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || os.Getenv(key) != "" {
			continue // already set — real env takes priority
		}
		os.Setenv(key, value)
	}
}
