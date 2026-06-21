package agents

import (
	"context"
	"testing"

	"mugi/internal/models"
	"mugi/internal/prompts"
	"mugi/internal/state"
)

// TestGenerationProfilesAreValid guards against a misconfigured agent profile
// (by construction: the profiles the agents actually ship with must be valid).
func TestGenerationProfilesAreValid(t *testing.T) {
	for _, p := range []generationProfile{plannerProfile, coderProfile, reviewerProfile} {
		if err := p.validate(); err != nil {
			t.Errorf("shipped profile %q is invalid: %v", p.role, err)
		}
	}
}

func TestGenerationProfileValidateRejectsBadConfig(t *testing.T) {
	bad := []generationProfile{
		{role: "", maxTokens: 10, attempts: 1},  // empty role
		{role: "x", maxTokens: 0, attempts: 1},   // no token budget
		{role: "x", maxTokens: 10, attempts: 0},  // no attempts
		{role: "x", maxTokens: -1, attempts: 1},  // negative budget
	}
	for _, p := range bad {
		if err := p.validate(); err == nil {
			t.Errorf("expected profile %+v to be rejected", p)
		}
	}
}

// TestPlannerRejectsEmptyTask verifies the planner validates its input at the
// boundary and fails fast — without spending an LLM call — on an empty task.
func TestPlannerRejectsEmptyTask(t *testing.T) {
	sp := &stubProvider{responses: []string{validPlanJSON}}
	p := NewPlanner(sp, prompts.NewLoader(""))

	st := state.New(&models.Task{ID: "t", Description: "   "}, 3)
	if err := p.Process(context.Background(), st); err == nil {
		t.Fatal("expected planner to reject an empty task description")
	}
	if sp.calls != 0 {
		t.Fatalf("planner must not call the LLM for an invalid task; calls=%d", sp.calls)
	}
}

// TestCoderRejectsInvalidPlanInState verifies the coder re-validates the plan it
// reads from shared state (an extra safeguard) and fails fast on a bad one.
func TestCoderRejectsInvalidPlanInState(t *testing.T) {
	sp := &stubProvider{responses: []string{goodArtifact}}
	c := NewCoder(sp, prompts.NewLoader(""))

	st := state.New(&models.Task{ID: "t", Description: "build x"}, 3)
	st.SetPlan(&models.Plan{Summary: "empty"}) // invalid: no steps
	if err := c.Process(context.Background(), st); err == nil {
		t.Fatal("expected coder to reject an invalid plan in state")
	}
	if sp.calls != 0 {
		t.Fatalf("coder must not call the LLM with an invalid plan; calls=%d", sp.calls)
	}
}
