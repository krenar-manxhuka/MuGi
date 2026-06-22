// Package integration_test runs an end-to-end pipeline using the mock provider.
// No real LLM API is called; the mock is configured to produce valid, realistic
// responses so every layer (prompt rendering, JSON parsing, state management,
// orchestration) is exercised together.
package integration_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"mugi/internal/agents"
	"mugi/internal/llm"
	"mugi/internal/models"
	"mugi/internal/orchestrator"
	"mugi/internal/prompts"
)

func buildFullPipeline(t *testing.T, maxRevisions int) *orchestrator.Orchestrator {
	t.Helper()
	loader := prompts.NewLoader("") // use embedded templates
	provider := llm.NewMockProvider()

	return orchestrator.New(
		agents.NewCoordinator(provider, loader),
		agents.NewPlanner(provider, loader),
		agents.NewCoder(provider, loader),
		agents.NewReviewer(provider, loader),
		orchestrator.Config{MaxRevisions: maxRevisions},
	)
}

// TestFullWorkflowHappyPath exercises every agent in sequence and asserts the
// state is coherent at the end.
func TestFullWorkflowHappyPath(t *testing.T) {
	orch := buildFullPipeline(t, 3)

	task := &models.Task{
		ID:          "integration-001",
		Description: "Build a minimal Go HTTP server with a /health endpoint",
		CreatedAt:   time.Now(),
	}

	st, err := orch.Run(context.Background(), task)
	if err != nil {
		t.Fatalf("workflow failed: %v", err)
	}

	// Status
	if st.GetStatus() != models.StatusCompleted {
		t.Errorf("expected status completed, got %s", st.GetStatus())
	}

	// Plan
	plan := st.GetPlan()
	if plan == nil {
		t.Fatal("plan is nil")
	}
	if plan.Summary == "" {
		t.Error("plan.Summary is empty")
	}
	if len(plan.Steps) == 0 {
		t.Error("plan has no steps")
	}

	// Artifact
	artifact := st.GetArtifact()
	if artifact == nil {
		t.Fatal("artifact is nil")
	}
	if artifact.Revision <= 0 {
		t.Errorf("artifact revision should be > 0, got %d", artifact.Revision)
	}
	if len(artifact.Files) == 0 {
		t.Error("artifact has no files")
	}
	for _, f := range artifact.Files {
		if f.Path == "" {
			t.Error("artifact file has empty path")
		}
		if f.Content == "" {
			t.Errorf("artifact file %q has empty content", f.Path)
		}
	}

	// Review
	reviews := st.AllReviews()
	if len(reviews) == 0 {
		t.Fatal("no reviews recorded")
	}
	lastReview := reviews[len(reviews)-1]
	if lastReview.Score < 0 || lastReview.Score > 10 {
		t.Errorf("review score out of range: %d", lastReview.Score)
	}

	// Log
	log := st.GetLog()
	agentsSeen := map[string]bool{}
	for _, entry := range log {
		agentsSeen[entry.Agent] = true
	}
	for _, expected := range []string{"coordinator", "planner", "coder", "reviewer"} {
		if !agentsSeen[expected] {
			t.Errorf("no log entry from agent %q", expected)
		}
	}
}

// TestFullWorkflowTimings verifies the workflow completes in reasonable time
// (mock calls are synchronous and fast).
func TestFullWorkflowTimings(t *testing.T) {
	orch := buildFullPipeline(t, 1)
	task := &models.Task{ID: "timing-001", Description: "timing test", CreatedAt: time.Now()}

	start := time.Now()
	_, err := orch.Run(context.Background(), task)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("workflow failed: %v", err)
	}
	// Mock provider is synchronous; the full pipeline should run in well under 1s.
	if elapsed > 5*time.Second {
		t.Errorf("workflow took too long: %s", elapsed)
	}
}

// TestFullWorkflowContextCancellation ensures the workflow respects context
// cancellation.  We cancel immediately to check the error propagates cleanly.
func TestFullWorkflowContextCancellation(t *testing.T) {
	// Use a provider that blocks until context is done
	blocking := &blockingProvider{}
	loader := prompts.NewLoader("")
	orch := orchestrator.New(
		agents.NewCoordinator(blocking, loader),
		agents.NewPlanner(blocking, loader),
		agents.NewCoder(blocking, loader),
		agents.NewReviewer(blocking, loader),
		orchestrator.Config{MaxRevisions: 1},
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	task := &models.Task{ID: "cancel-001", Description: "cancel test", CreatedAt: time.Now()}
	_, err := orch.Run(ctx, task)
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
}

// TestWorkflowStateJSONMarshal verifies that the final state can be serialised
// to JSON without error (important for persistence and API responses).
func TestWorkflowStateJSONMarshal(t *testing.T) {
	orch := buildFullPipeline(t, 1)
	task := &models.Task{ID: "json-001", Description: "json marshal test", CreatedAt: time.Now()}
	st, err := orch.Run(context.Background(), task)
	if err != nil {
		t.Fatalf("workflow failed: %v", err)
	}

	// Collect the serialisable parts of the state
	snapshot := struct {
		Task     *models.Task      `json:"task"`
		Plan     *models.Plan      `json:"plan"`
		Artifact *models.Artifact  `json:"artifact"`
		Reviews  []*models.Review  `json:"reviews"`
		Log      []models.LogEntry `json:"log"`
	}{
		Task:     st.Task,
		Plan:     st.GetPlan(),
		Artifact: st.GetArtifact(),
		Reviews:  st.AllReviews(),
		Log:      st.GetLog(),
	}

	b, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	if len(b) == 0 {
		t.Fatal("marshalled JSON is empty")
	}
}

// blockingProvider blocks until the context is cancelled, used to test cancellation.
type blockingProvider struct{}

func (b *blockingProvider) Name() string { return "blocking" }

func (b *blockingProvider) Generate(ctx context.Context, _ llm.Request) (llm.Response, error) {
	<-ctx.Done()
	return llm.Response{}, ctx.Err()
}
