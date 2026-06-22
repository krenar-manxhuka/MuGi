package unit_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"mugi/internal/agents"
	"mugi/internal/llm"
	"mugi/internal/models"
	"mugi/internal/orchestrator"
	"mugi/internal/prompts"
)

func buildOrchestrator(provider llm.Provider, maxRevisions int) *orchestrator.Orchestrator {
	loader := prompts.NewLoader("")
	return orchestrator.New(
		agents.NewCoordinator(provider, loader),
		agents.NewPlanner(provider, loader),
		agents.NewCoder(provider, loader),
		agents.NewReviewer(provider, loader),
		orchestrator.Config{MaxRevisions: maxRevisions},
	)
}

func newTask(desc string) *models.Task {
	return &models.Task{
		ID:          "orch-test-001",
		Description: desc,
		CreatedAt:   time.Now(),
	}
}

// TestOrchestratorRunSuccess verifies the happy path: mock approves on first pass.
func TestOrchestratorRunSuccess(t *testing.T) {
	orch := buildOrchestrator(llm.NewMockProvider(), 3)
	st, err := orch.Run(context.Background(), newTask("Build a simple Go HTTP server"))
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if st.GetStatus() != models.StatusCompleted {
		t.Fatalf("expected status completed, got %s", st.GetStatus())
	}
	if st.GetPlan() == nil {
		t.Fatal("expected plan to be set")
	}
	if st.GetArtifact() == nil {
		t.Fatal("expected artifact to be set")
	}
	if len(st.AllReviews()) == 0 {
		t.Fatal("expected at least one review")
	}
}

// TestOrchestratorBoundedRevisions verifies that the loop terminates after
// MaxRevisions even when the reviewer always rejects.
func TestOrchestratorBoundedRevisions(t *testing.T) {
	// Reviewer always rejects
	rejectingReview, _ := json.Marshal(models.Review{
		Approved: false,
		Score:    4,
		Feedback: "Not good enough.",
		Issues: []models.Issue{
			{Severity: "major", Description: "missing tests", Suggestion: "add tests"},
		},
		Revision: 1,
	})

	planJSON, _ := json.Marshal(models.Plan{
		Summary: "test plan",
		Steps:   []models.Step{{ID: 1, Title: "write code"}},
	})
	artifactJSON, _ := json.Marshal(models.Artifact{
		Summary:  "test artifact",
		Revision: 1,
		Files:    []models.File{{Path: "main.go", Lang: "go", Content: "package main"}},
	})

	provider := &llm.MockProvider{
		CustomResponses: map[string]string{
			"coordinator": "Processing...",
			"planner":     string(planJSON),
			"coder":       string(artifactJSON),
			"reviewer":    string(rejectingReview),
		},
	}

	maxRevisions := 2
	orch := buildOrchestrator(provider, maxRevisions)
	st, err := orch.Run(context.Background(), newTask("Build something"))
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	// Should complete (not fail) even though reviewer kept rejecting
	if st.GetStatus() != models.StatusCompleted {
		t.Fatalf("expected completed, got %s", st.GetStatus())
	}

	iter, _, _ := st.Snapshot()
	if iter > maxRevisions {
		t.Fatalf("exceeded max revisions: got %d, max %d", iter, maxRevisions)
	}
}

// TestOrchestratorPropagatesPlannerError verifies that a planner failure
// results in a failed workflow state (not a panic or silent skip).
func TestOrchestratorPropagatesPlannerError(t *testing.T) {
	provider := &llm.MockProvider{
		CustomResponses: map[string]string{
			"coordinator": "ok",
			// planner returns invalid JSON → parse error
			"planner":  "this is not json at all",
			"coder":    `{"summary":"x","revision":1,"files":[]}`,
			"reviewer": `{"approved":true,"score":10,"feedback":"ok","revision":1}`,
		},
	}

	orch := buildOrchestrator(provider, 3)
	st, err := orch.Run(context.Background(), newTask("Trigger planner error"))
	if err == nil {
		t.Fatal("expected Run to return an error when planner produces invalid JSON")
	}
	if st.GetStatus() != models.StatusFailed {
		t.Fatalf("expected status failed, got %s", st.GetStatus())
	}
}

// TestOrchestratorLogEntries verifies that each agent writes at least one log entry.
func TestOrchestratorLogEntries(t *testing.T) {
	orch := buildOrchestrator(llm.NewMockProvider(), 1)
	st, err := orch.Run(context.Background(), newTask("Log entry test"))
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	roles := map[string]bool{}
	for _, entry := range st.GetLog() {
		roles[entry.Agent] = true
	}

	for _, expected := range []string{"coordinator", "planner", "coder", "reviewer"} {
		if !roles[expected] {
			t.Errorf("no log entry from agent %q", expected)
		}
	}
}

// TestOrchestratorSecondPassApproves simulates a reviewer that rejects on the
// first pass and approves on the second.
func TestOrchestratorSecondPassApproves(t *testing.T) {
	callCount := 0

	reject, _ := json.Marshal(models.Review{
		Approved: false, Score: 5, Feedback: "needs work", Revision: 1,
		Issues: []models.Issue{{Severity: "major", Description: "missing tests", Suggestion: "add them"}},
	})
	approve, _ := json.Marshal(models.Review{
		Approved: true, Score: 9, Feedback: "looks good", Revision: 2,
	})
	planJSON, _ := json.Marshal(models.Plan{
		Summary: "plan", Steps: []models.Step{{ID: 1, Title: "step"}},
	})

	// Custom provider that alternates reviewer responses
	provider := &alternatingReviewerProvider{
		base: &llm.MockProvider{
			CustomResponses: map[string]string{
				"coordinator": "ok",
				"planner":     string(planJSON),
			},
		},
		coderResponse:   fmt.Sprintf(`{"summary":"code","revision":%d,"files":[{"path":"main.go","lang":"go","content":"package main"}]}`, 1),
		reviewResponses: []string{string(reject), string(approve)},
		callCount:       &callCount,
	}

	orch := buildOrchestrator(provider, 3)
	st, err := orch.Run(context.Background(), newTask("Two-pass approval"))
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if st.GetStatus() != models.StatusCompleted {
		t.Fatalf("expected completed, got %s", st.GetStatus())
	}

	reviews := st.AllReviews()
	if len(reviews) < 2 {
		t.Fatalf("expected at least 2 reviews, got %d", len(reviews))
	}
	if !reviews[len(reviews)-1].Approved {
		t.Fatal("expected final review to be approved")
	}
}

// TestOrchestratorObjectiveGateOverridesFalseApproval verifies the objective
// gate: when go build/test is red, a reviewer "approved" verdict must NOT ship
// the artifact. Instead the orchestrator keeps revising up to the cap and records
// the false approval. This is the behavioural fix for the bench finding that the
// LLM reviewer rubber-stamps code that fails its own tests.
func TestOrchestratorObjectiveGateOverridesFalseApproval(t *testing.T) {
	planJSON, _ := json.Marshal(models.Plan{
		Summary: "plan", Steps: []models.Step{{ID: 1, Title: "implement"}},
	})
	// Compiles cleanly, but the test asserts a falsehood so `go test` fails.
	artifactJSON, _ := json.Marshal(models.Artifact{
		Summary:  "adder",
		Revision: 1,
		Files: []models.File{
			{Path: "go.mod", Lang: "text", Content: "module generated\n\ngo 1.21\n"},
			{Path: "add.go", Lang: "go", Content: "package main\n\nfunc Add(a, b int) int { return a + b }\n\nfunc main() {}\n"},
			{Path: "add_test.go", Lang: "go", Content: "package main\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 1) != 3 {\n\t\tt.Fatal(\"boom\")\n\t}\n}\n"},
		},
	})
	// The reviewer always approves at 9/10 — exactly the failure mode under test.
	approve, _ := json.Marshal(models.Review{
		Approved: true, Score: 9, Feedback: "looks great", Revision: 1,
	})

	provider := &llm.MockProvider{
		CustomResponses: map[string]string{
			"you are the coordinator agent": "ok",
			"you are the planner agent":     string(planJSON),
			"you are the coder agent":       string(artifactJSON),
			"you are the reviewer agent":    string(approve),
		},
	}

	loader := prompts.NewLoader("")
	maxRevisions := 2
	orch := orchestrator.New(
		agents.NewCoordinator(provider, loader),
		agents.NewPlanner(provider, loader),
		agents.NewCoder(provider, loader),
		agents.NewReviewer(provider, loader),
		orchestrator.Config{MaxRevisions: maxRevisions, RunTests: true},
	)

	st, err := orch.Run(context.Background(), newTask("Add two numbers"))
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if st.GetStatus() != models.StatusCompleted {
		t.Fatalf("expected completed, got %s", st.GetStatus())
	}

	exec := st.GetExecResult()
	if exec == nil || exec.Skipped {
		t.Fatalf("expected a real exec result, got %+v", exec)
	}
	if !exec.BuildOK {
		t.Fatalf("precondition: artifact should build; build output: %s", exec.BuildOut)
	}
	if exec.TestOK {
		t.Fatal("precondition: the artifact's tests were expected to FAIL")
	}

	if st.FalseApprovals() < 1 {
		t.Fatalf("objective gate should have recorded >=1 false approval, got %d", st.FalseApprovals())
	}
	// Despite the reviewer approving, the gate must have driven the loop to the cap.
	iter, _, _ := st.Snapshot()
	if iter < maxRevisions {
		t.Fatalf("expected the gate to use all %d revisions, got %d", maxRevisions, iter)
	}
}

// alternatingReviewerProvider is a test helper that cycles through a list of
// reviewer responses while delegating everything else to a base provider.
type alternatingReviewerProvider struct {
	base            llm.Provider
	coderResponse   string
	reviewResponses []string
	callCount       *int
	reviewCallCount int
}

func (p *alternatingReviewerProvider) Name() string { return "alternating-test" }

func (p *alternatingReviewerProvider) Generate(ctx context.Context, req llm.Request) (llm.Response, error) {
	// Use the opening sentence of each template as the routing key to avoid
	// false positives (e.g. the coordinator template renders "Reviewer feedback:"
	// after the first review, which would incorrectly match a plain "reviewer" check).
	switch {
	case containsCI(req.SystemPrompt, "you are the coder agent"):
		*p.callCount++
		rev := *p.callCount
		artifactJSON, _ := json.Marshal(models.Artifact{
			Summary:  "code",
			Revision: rev,
			Files:    []models.File{{Path: "main.go", Lang: "go", Content: "package main"}},
		})
		return llm.Response{Content: string(artifactJSON)}, nil

	case containsCI(req.SystemPrompt, "you are the reviewer agent"):
		idx := p.reviewCallCount
		p.reviewCallCount++
		if idx >= len(p.reviewResponses) {
			idx = len(p.reviewResponses) - 1
		}
		return llm.Response{Content: p.reviewResponses[idx]}, nil

	default:
		return p.base.Generate(ctx, req)
	}
}

// containsCI is a case-insensitive substring check used by the test provider
// to route requests based on what the system prompt says about the agent's role.
func containsCI(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}
