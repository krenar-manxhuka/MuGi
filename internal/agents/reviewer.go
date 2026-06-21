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

// reviewerProfile is the Reviewer's generation contract. Detailed reviews
// (feedback + several issues) need headroom so a thorough review isn't truncated
// into invalid JSON. A reviewer failure is non-fatal (the orchestrator keeps the
// current artifact), but one resample recovers a transient bad sample cheaply.
var reviewerProfile = generationProfile{
	role:        "reviewer",
	maxTokens:   4096,
	temperature: 0.1,
	attempts:    2,
}

// Reviewer inspects the Coder's artifact and decides whether it is ready to
// ship or should be sent back for revision.
type Reviewer struct {
	llmAgent
}

// NewReviewer returns a Reviewer backed by the given provider and loader.
func NewReviewer(provider llm.Provider, loader *prompts.Loader) *Reviewer {
	return &Reviewer{llmAgent{provider: provider, loader: loader}}
}

func (r *Reviewer) Role() string { return reviewerProfile.role }

// reviewerData is the template context for reviewer.tmpl.
type reviewerData struct {
	Task         *models.Task
	Artifact     *models.Artifact
	ArtifactJSON string
	ExecResult   *models.ExecResult
}

func (r *Reviewer) Process(ctx context.Context, st *state.WorkflowState) error {
	st.SetStatus(models.StatusReviewing)

	artifact := st.GetArtifact()
	if artifact == nil {
		return fmt.Errorf("reviewer: no artifact available in state")
	}
	// As an extra safeguard: re-validate the artifact reached us intact through
	// mutable shared state before reviewing (and writing) it.
	if err := artifact.Validate(); err != nil {
		return fmt.Errorf("reviewer: invalid artifact in state: %w", err)
	}

	artifactBytes, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return fmt.Errorf("reviewer: marshal artifact: %w", err)
	}

	data := reviewerData{
		Task:         st.Task,
		Artifact:     artifact,
		ArtifactJSON: string(artifactBytes),
		ExecResult:   st.GetExecResult(),
	}

	sysPrompt, err := r.loader.Render("reviewer", data)
	if err != nil {
		return fmt.Errorf("reviewer: render prompt: %w", err)
	}

	userMsg := fmt.Sprintf("Review revision %d of the artifact.", artifact.Revision)
	review, err := generateFor[models.Review, *models.Review](ctx, r.llmAgent, reviewerProfile, sysPrompt, userMsg)
	if err != nil {
		return err
	}

	st.AddReview(review)
	verdict := "needs revision"
	if review.Approved {
		verdict = "APPROVED"
	}
	st.AddLog("reviewer", fmt.Sprintf("revision %d reviewed: %s (score %d/10, %d issues)",
		review.Revision, verdict, review.Score, len(review.Issues)))
	return nil
}
