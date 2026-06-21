package unit_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"mugi/internal/agents"
	"mugi/internal/llm"
	"mugi/internal/models"
	"mugi/internal/prompts"
	"mugi/internal/state"
)

// newTestState returns a minimal WorkflowState for use in agent tests.
func newTestState() *state.WorkflowState {
	return state.New(&models.Task{
		ID:          "test-001",
		Description: "Build a simple Go HTTP server",
		CreatedAt:   time.Now(),
	}, 3)
}

// newMockLoader returns a Loader that uses only embedded templates (no FS dir).
func newMockLoader() *prompts.Loader {
	return prompts.NewLoader("")
}

// ── Coordinator ───────────────────────────────────────────────────────────────

func TestCoordinatorRole(t *testing.T) {
	c := agents.NewCoordinator(llm.NewMockProvider(), newMockLoader())
	if c.Role() != "coordinator" {
		t.Fatalf("expected 'coordinator', got %q", c.Role())
	}
}

func TestCoordinatorProcess(t *testing.T) {
	c := agents.NewCoordinator(llm.NewMockProvider(), newMockLoader())
	st := newTestState()

	if err := c.Process(context.Background(), st); err != nil {
		t.Fatalf("coordinator.Process returned error: %v", err)
	}

	log := st.GetLog()
	if len(log) == 0 {
		t.Fatal("expected at least one log entry after coordinator.Process")
	}
	if log[0].Agent != "coordinator" {
		t.Fatalf("expected log from coordinator, got %q", log[0].Agent)
	}
}

// ── Planner ───────────────────────────────────────────────────────────────────

func TestPlannerRole(t *testing.T) {
	p := agents.NewPlanner(llm.NewMockProvider(), newMockLoader())
	if p.Role() != "planner" {
		t.Fatalf("expected 'planner', got %q", p.Role())
	}
}

func TestPlannerProcess(t *testing.T) {
	p := agents.NewPlanner(llm.NewMockProvider(), newMockLoader())
	st := newTestState()

	if err := p.Process(context.Background(), st); err != nil {
		t.Fatalf("planner.Process returned error: %v", err)
	}

	plan := st.GetPlan()
	if plan == nil {
		t.Fatal("expected plan to be set after planner.Process")
	}
	if plan.Summary == "" {
		t.Fatal("expected non-empty plan summary")
	}
	if len(plan.Steps) == 0 {
		t.Fatal("expected at least one step in plan")
	}
}

func TestPlannerSetsStatusPlanning(t *testing.T) {
	p := agents.NewPlanner(llm.NewMockProvider(), newMockLoader())
	st := newTestState()
	_ = p.Process(context.Background(), st) //nolint:errcheck
	if st.GetStatus() != models.StatusPlanning {
		t.Fatalf("expected status planning, got %s", st.GetStatus())
	}
}

// ── Coder ─────────────────────────────────────────────────────────────────────

func TestCoderRole(t *testing.T) {
	c := agents.NewCoder(llm.NewMockProvider(), newMockLoader())
	if c.Role() != "coder" {
		t.Fatalf("expected 'coder', got %q", c.Role())
	}
}

func TestCoderRequiresPlan(t *testing.T) {
	c := agents.NewCoder(llm.NewMockProvider(), newMockLoader())
	st := newTestState()
	// No plan set — should fail
	if err := c.Process(context.Background(), st); err == nil {
		t.Fatal("expected error when no plan is set")
	}
}

func TestCoderProcess(t *testing.T) {
	c := agents.NewCoder(llm.NewMockProvider(), newMockLoader())
	st := newTestState()

	// Pre-populate plan
	st.SetPlan(&models.Plan{
		Summary: "Build a simple server",
		Steps:   []models.Step{{ID: 1, Title: "Write main.go"}},
	})

	if err := c.Process(context.Background(), st); err != nil {
		t.Fatalf("coder.Process returned error: %v", err)
	}

	artifact := st.GetArtifact()
	if artifact == nil {
		t.Fatal("expected artifact to be set after coder.Process")
	}
	if len(artifact.Files) == 0 {
		t.Fatal("expected at least one file in artifact")
	}
}

// ── Reviewer ──────────────────────────────────────────────────────────────────

func TestReviewerRole(t *testing.T) {
	r := agents.NewReviewer(llm.NewMockProvider(), newMockLoader())
	if r.Role() != "reviewer" {
		t.Fatalf("expected 'reviewer', got %q", r.Role())
	}
}

func TestReviewerRequiresArtifact(t *testing.T) {
	r := agents.NewReviewer(llm.NewMockProvider(), newMockLoader())
	st := newTestState()
	if err := r.Process(context.Background(), st); err == nil {
		t.Fatal("expected error when no artifact is set")
	}
}

func TestReviewerProcess(t *testing.T) {
	r := agents.NewReviewer(llm.NewMockProvider(), newMockLoader())
	st := newTestState()

	st.SetArtifact(&models.Artifact{
		Summary:  "simple server",
		Revision: 1,
		Files: []models.File{
			{Path: "main.go", Lang: "go", Content: "package main"},
		},
	})

	if err := r.Process(context.Background(), st); err != nil {
		t.Fatalf("reviewer.Process returned error: %v", err)
	}

	review := st.LatestReview()
	if review == nil {
		t.Fatal("expected review to be added after reviewer.Process")
	}
	if review.Score < 0 || review.Score > 10 {
		t.Fatalf("review score out of range: %d", review.Score)
	}
}

// ── JSON extraction edge cases ────────────────────────────────────────────────

// TestPlannerAcceptsMarkdownWrappedJSON verifies the extractJSON helper works
// by injecting a mock that returns ```json-wrapped content.
func TestPlannerAcceptsMarkdownWrappedJSON(t *testing.T) {
	plan := models.Plan{
		Summary: "wrapped plan",
		Steps:   []models.Step{{ID: 1, Title: "step one"}},
	}
	planBytes, _ := json.Marshal(plan)

	mock := &llm.MockProvider{
		CustomResponses: map[string]string{
			"planner": "```json\n" + string(planBytes) + "\n```",
		},
	}

	p := agents.NewPlanner(mock, newMockLoader())
	st := newTestState()
	if err := p.Process(context.Background(), st); err != nil {
		t.Fatalf("planner failed with markdown-wrapped JSON: %v", err)
	}
	if st.GetPlan() == nil {
		t.Fatal("plan not set")
	}
}

// TestPlannerAcceptsStringDependsOn reproduces the qwen2.5-coder planner
// failure: depends_on emitted as JSON strings instead of ints. The pipeline
// must parse it rather than erroring out before the coder runs.
func TestPlannerAcceptsStringDependsOn(t *testing.T) {
	mock := &llm.MockProvider{
		CustomResponses: map[string]string{
			"planner": `{
  "summary": "FizzBuzz",
  "steps": [
    {"id": 1, "title": "module", "depends_on": []},
    {"id": 2, "title": "impl", "depends_on": ["1"]}
  ]
}`,
		},
	}

	p := agents.NewPlanner(mock, newMockLoader())
	st := newTestState()
	if err := p.Process(context.Background(), st); err != nil {
		t.Fatalf("planner failed with string depends_on: %v", err)
	}
	plan := st.GetPlan()
	if plan == nil {
		t.Fatal("plan not set")
	}
	if len(plan.Steps) != 2 || len(plan.Steps[1].DependsOn) != 1 || plan.Steps[1].DependsOn[0] != 1 {
		t.Fatalf("depends_on not parsed: %#v", plan.Steps)
	}
}

// TestCoderAcceptsRawControlCharsInContent reproduces the qwen2.5-coder coder
// failure: source code pasted into a "content" field with literal newlines and
// tabs instead of \n and \t. The artifact must parse and preserve the code.
func TestCoderAcceptsRawControlCharsInContent(t *testing.T) {
	const code = "package fib\n\nimport \"fmt\"\n\nfunc Fib(n int) int {\n\treturn n\n}\n"
	// JSON with a raw (unescaped) newline+tab body but escaped inner quotes —
	// exactly the shape the model produced.
	rawJSON := "{\n  \"summary\": \"fib\",\n  \"revision\": 1,\n  \"files\": [\n" +
		"    {\"path\": \"fib.go\", \"lang\": \"go\", \"content\": \"" +
		"package fib\n\nimport \\\"fmt\\\"\n\nfunc Fib(n int) int {\n\treturn n\n}\n" +
		"\"}\n  ]\n}"

	mock := &llm.MockProvider{
		CustomResponses: map[string]string{"coder": rawJSON},
	}

	c := agents.NewCoder(mock, newMockLoader())
	st := newTestState()
	st.SetPlan(&models.Plan{Summary: "fib", Steps: []models.Step{{ID: 1, Title: "impl"}}})

	if err := c.Process(context.Background(), st); err != nil {
		t.Fatalf("coder failed with raw control chars in content: %v", err)
	}
	art := st.GetArtifact()
	if art == nil || len(art.Files) != 1 {
		t.Fatalf("artifact not parsed: %#v", art)
	}
	if art.Files[0].Content != code {
		t.Fatalf("content not preserved:\nwant %q\ngot  %q", code, art.Files[0].Content)
	}
}
