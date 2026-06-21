package agents

import (
	"context"
	"fmt"

	"mugi/internal/llm"
	"mugi/internal/models"
	"mugi/internal/prompts"
	"mugi/internal/state"
)

// Coordinator narrates the workflow, surfaces progress to the operator, and
// decides (via its LLM call) whether output is ready or needs revision.
// Routing decisions are enforced by the orchestrator; the coordinator
// contributes the human-readable rationale logged alongside each decision.
//
// Unlike the structured agents it produces free-form prose, not validated JSON,
// so it issues its LLM request directly rather than through generateFor.
type Coordinator struct {
	llmAgent
}

// NewCoordinator returns a Coordinator backed by the given provider and loader.
func NewCoordinator(provider llm.Provider, loader *prompts.Loader) *Coordinator {
	return &Coordinator{llmAgent{provider: provider, loader: loader}}
}

func (c *Coordinator) Role() string { return "coordinator" }

// coordinatorData is the template context for coordinator.tmpl.
type coordinatorData struct {
	Task      *models.Task
	Status    models.WorkflowStatus
	Iteration int
	MaxIter   int
	Plan      *models.Plan
	Artifact  *models.Artifact
	Review    *models.Review
}

func (c *Coordinator) Process(ctx context.Context, st *state.WorkflowState) error {
	iter, maxIter, status := st.Snapshot()

	data := coordinatorData{
		Task:      st.Task,
		Status:    status,
		Iteration: iter,
		MaxIter:   maxIter,
		Plan:      st.GetPlan(),
		Artifact:  st.GetArtifact(),
		Review:    st.LatestReview(),
	}

	sysPrompt, err := c.loader.Render("coordinator", data)
	if err != nil {
		return fmt.Errorf("coordinator: render prompt: %w", err)
	}

	resp, err := c.provider.Generate(ctx, llm.Request{
		SystemPrompt: sysPrompt,
		Messages: []llm.Message{
			{Role: "user", Content: fmt.Sprintf("Task: %s\n\nProvide a concise status update.", st.Task.Description)},
		},
		MaxTokens:   512,
		Temperature: 0.3,
	})
	if err != nil {
		return fmt.Errorf("coordinator: llm: %w", err)
	}

	st.AddLog("coordinator", resp.Content)
	return nil
}
