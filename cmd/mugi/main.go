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
	"mugi/internal/llm"
	"mugi/internal/models"
	"mugi/internal/orchestrator"
	"mugi/internal/prompts"
	"mugi/internal/state"
)

func main() {
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
	slog.Info("using LLM provider", "provider", provider.Name())

	loader := prompts.NewLoader(cfg.PromptsDir)

	coord := agents.NewCoordinator(provider, loader)
	plan := agents.NewPlanner(provider, loader)
	code := agents.NewCoder(provider, loader)
	rev := agents.NewReviewer(provider, loader)

	orch := orchestrator.New(coord, plan, code, rev, orchestrator.Config{
		MaxRevisions: cfg.MaxRevisions,
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

	// Write each source file
	for _, f := range artifact.Files {
		dest := filepath.Join(dir, f.Path)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(dest), err)
		}
		if err := os.WriteFile(dest, []byte(f.Content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", dest, err)
		}
	}
	return nil
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
