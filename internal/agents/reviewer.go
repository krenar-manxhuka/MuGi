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

// Reviewer inspects the Coder's artifact and decides whether it is ready to
// ship or should be sent back for revision.
type Reviewer struct {
	provider llm.Provider
	loader   *prompts.Loader
}

// NewReviewer returns a Reviewer backed by the given provider and loader.
func NewReviewer(provider llm.Provider, loader *prompts.Loader) *Reviewer {
	return &Reviewer{provider: provider, loader: loader}
}

func (r *Reviewer) Role() string { return "reviewer" }

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

	resp, err := r.provider.Generate(ctx, llm.Request{
		SystemPrompt: sysPrompt,
		Messages: []llm.Message{
			{Role: "user", Content: fmt.Sprintf("Review revision %d of the artifact.", artifact.Revision)},
		},
		MaxTokens:   2048,
		Temperature: 0.1,
	})
	if err != nil {
		return fmt.Errorf("reviewer: llm: %w", err)
	}

	raw := extractJSON(resp.Content)
	var review models.Review
	if err := json.Unmarshal([]byte(raw), &review); err != nil {
		preview := resp.Content
		if len(preview) > 300 {
			preview = preview[:300] + "…"
		}
		return fmt.Errorf("reviewer: parse review JSON: %w\nraw response (truncated):\n%s", err, preview)
	}

	st.AddReview(&review)
	verdict := "needs revision"
	if review.Approved {
		verdict = "APPROVED"
	}
	st.AddLog("reviewer", fmt.Sprintf("revision %d reviewed: %s (score %d/10, %d issues)",
		review.Revision, verdict, review.Score, len(review.Issues)))
	return nil
}
