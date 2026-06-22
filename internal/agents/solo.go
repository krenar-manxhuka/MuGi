package agents

import (
	"context"
	"fmt"

	"mugi/internal/llm"
	"mugi/internal/models"
	"mugi/internal/prompts"
	"mugi/internal/state"
)

// soloProfile mirrors the coder's budget: a single call must emit the full
// multi-file artifact (implementation + its own tests), so it needs the same
// large output ceiling — a smaller cap truncates the hardest tasks into invalid
// JSON.
var soloProfile = generationProfile{
	role:        "solo",
	maxTokens:   16384,
	temperature: 0.1,
	attempts:    2,
}

// SoloCoder is the single-call ablation agent: one LLM call turns the task
// straight into a complete artifact, with no planner, reviewer, or revision
// loop. It exists as the honest control for "does the multi-agent pipeline
// actually beat one well-prompted call?".
type SoloCoder struct {
	llmAgent
}

// NewSoloCoder returns a SoloCoder backed by the given provider and loader.
func NewSoloCoder(provider llm.Provider, loader *prompts.Loader) *SoloCoder {
	return &SoloCoder{llmAgent{provider: provider, loader: loader}}
}

func (c *SoloCoder) Role() string { return soloProfile.role }

// soloData is the template context for solo.tmpl.
type soloData struct {
	Task *models.Task
}

func (c *SoloCoder) Process(ctx context.Context, st *state.WorkflowState) error {
	// As an extra safeguard: validate the task at the boundary before acting on it.
	if err := st.Task.Validate(); err != nil {
		return fmt.Errorf("solo: %w", err)
	}
	st.SetStatus(models.StatusCoding)

	sysPrompt, err := c.loader.Render("solo", soloData{Task: st.Task})
	if err != nil {
		return fmt.Errorf("solo: render prompt: %w", err)
	}

	artifact, err := generateFor[models.Artifact, *models.Artifact](ctx, c.llmAgent, soloProfile,
		sysPrompt, "Build the project: "+st.Task.Description)
	if err != nil {
		return err
	}

	// Canonicalise Go sources before they are built or written out.
	formatGoSources(artifact)

	st.SetArtifact(artifact)
	st.AddLog("solo", fmt.Sprintf("single-call artifact produced (%d files): %s",
		len(artifact.Files), artifact.Summary))
	return nil
}
