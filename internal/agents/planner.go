package agents

import (
	"context"
	"encoding/json"
	"fmt"

	"mugi/internal/llm"
	"mugi/internal/models"
	"mugi/internal/prompts"
	"mugi/internal/state"
)

// Planner transforms the raw task description into a structured Plan that the
// Coder can consume directly.
type Planner struct {
	provider llm.Provider
	loader   *prompts.Loader
}

// NewPlanner returns a Planner backed by the given provider and loader.
func NewPlanner(provider llm.Provider, loader *prompts.Loader) *Planner {
	return &Planner{provider: provider, loader: loader}
}

func (p *Planner) Role() string { return "planner" }

// plannerData is the template context for planner.tmpl.
type plannerData struct {
	Task *models.Task
}

func (p *Planner) Process(ctx context.Context, st *state.WorkflowState) error {
	st.SetStatus(models.StatusPlanning)

	sysPrompt, err := p.loader.Render("planner", plannerData{Task: st.Task})
	if err != nil {
		return fmt.Errorf("planner: render prompt: %w", err)
	}

	resp, err := p.provider.Generate(ctx, llm.Request{
		SystemPrompt: sysPrompt,
		Messages: []llm.Message{
			{Role: "user", Content: "Create the execution plan for: " + st.Task.Description},
		},
		MaxTokens:   2048,
		Temperature: 0.2,
	})
	if err != nil {
		return fmt.Errorf("planner: llm: %w", err)
	}

	raw := extractJSON(resp.Content)
	var plan models.Plan
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		return fmt.Errorf("planner: parse plan JSON: %w\nraw response:\n%s", err, resp.Content)
	}

	st.SetPlan(&plan)
	st.AddLog("planner", fmt.Sprintf("plan created: %s (%d steps)", plan.Summary, len(plan.Steps)))
	return nil
}
