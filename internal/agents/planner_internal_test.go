package agents

import (
	"context"
	"testing"

	"mugi/internal/llm"
	"mugi/internal/models"
	"mugi/internal/prompts"
	"mugi/internal/state"
)

// stubProvider returns a fixed sequence of responses, one per Generate call.
// After the sequence is exhausted it repeats the last entry.
type stubProvider struct {
	responses []string
	calls     int
}

func (s *stubProvider) Name() string { return "stub" }

func (s *stubProvider) Generate(_ context.Context, _ llm.Request) (llm.Response, error) {
	i := s.calls
	s.calls++
	if i >= len(s.responses) {
		i = len(s.responses) - 1
	}
	return llm.Response{Content: s.responses[i]}, nil
}

func newPlannerState() *state.WorkflowState {
	return state.New(&models.Task{ID: "t1", Description: "build a thing"}, 3)
}

const validPlanJSON = `{"summary":"do it","steps":[{"id":1,"title":"step","description":"detail"}]}`

// invalidPlanJSON reproduces the observed failure mode: an unescaped double
// quote inside a string value ({"status":"ok"} pasted verbatim), which
// prematurely terminates the JSON string.
const invalidPlanJSON = `{"summary":"emit {"status":"ok"} exactly","steps":[]}`

// TestPlannerRetriesOnInvalidJSON verifies a transient bad-JSON sample is
// retried and the task proceeds when the second sample parses.
func TestPlannerRetriesOnInvalidJSON(t *testing.T) {
	sp := &stubProvider{responses: []string{invalidPlanJSON, validPlanJSON}}
	p := NewPlanner(sp, prompts.NewLoader(""))

	st := newPlannerState()
	if err := p.Process(context.Background(), st); err != nil {
		t.Fatalf("expected planner to recover on retry, got: %v", err)
	}
	if sp.calls != 2 {
		t.Fatalf("expected 2 attempts, got %d", sp.calls)
	}
	if plan := st.GetPlan(); plan == nil || plan.Summary != "do it" {
		t.Fatalf("expected plan to be set from the valid sample, got %+v", plan)
	}
}

// TestPlannerFailsAfterExhaustingRetries verifies a persistently bad planner
// still fails (with a parse error), bounded by the profile's attempts.
func TestPlannerFailsAfterExhaustingRetries(t *testing.T) {
	sp := &stubProvider{responses: []string{invalidPlanJSON}}
	p := NewPlanner(sp, prompts.NewLoader(""))

	st := newPlannerState()
	err := p.Process(context.Background(), st)
	if err == nil {
		t.Fatal("expected a parse error after exhausting retries")
	}
	if sp.calls != plannerProfile.attempts {
		t.Fatalf("expected %d attempts, got %d", plannerProfile.attempts, sp.calls)
	}
	if st.GetPlan() != nil {
		t.Fatal("plan must remain unset on total failure")
	}
}
