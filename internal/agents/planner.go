package agents

import (
	"context"
	"fmt"

	"mugi/internal/llm"
	"mugi/internal/models"
	"mugi/internal/prompts"
	"mugi/internal/state"
)

// plannerProfile is the Planner's generation contract. Headroom is generous
// because a smaller cap truncated hard-tier plans (many detailed steps + risks +
// assumptions) into invalid JSON; the plan has no downstream fallback, so one
// resample makes a transient malformed sample non-fatal.
var plannerProfile = generationProfile{
	role:        "planner",
	maxTokens:   8192,
	temperature: 0.2,
	attempts:    2,
}

// Planner transforms the raw task description into a structured Plan that the
// Coder can consume directly.
type Planner struct {
	llmAgent
}

// NewPlanner returns a Planner backed by the given provider and loader.
func NewPlanner(provider llm.Provider, loader *prompts.Loader) *Planner {
	return &Planner{llmAgent{provider: provider, loader: loader}}
}

func (p *Planner) Role() string { return plannerProfile.role }

// plannerData is the template context for planner.tmpl.
type plannerData struct {
	Task *models.Task
}

func (p *Planner) Process(ctx context.Context, st *state.WorkflowState) error {
	// As an extra safeguard: validate the task at the boundary before acting on it,
	// even though it was constructed upstream.
	if err := st.Task.Validate(); err != nil {
		return fmt.Errorf("planner: %w", err)
	}
	st.SetStatus(models.StatusPlanning)

	sysPrompt, err := p.loader.Render("planner", plannerData{Task: st.Task})
	if err != nil {
		return fmt.Errorf("planner: render prompt: %w", err)
	}

	plan, err := generateFor[models.Plan, *models.Plan](ctx, p.llmAgent, plannerProfile,
		sysPrompt, "Create the execution plan for: "+st.Task.Description)
	if err != nil {
		return err
	}

	st.SetPlan(plan)
	st.AddLog("planner", fmt.Sprintf("plan created: %s (%d steps)", plan.Summary, len(plan.Steps)))
	return nil
}
