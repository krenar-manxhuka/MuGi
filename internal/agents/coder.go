package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"mugi/internal/llm"
	"mugi/internal/models"
	"mugi/internal/prompts"
	"mugi/internal/state"
)

// Coder implements the plan produced by the Planner and revises its output
// based on feedback from the Reviewer.
type Coder struct {
	provider llm.Provider
	loader   *prompts.Loader
}

// NewCoder returns a Coder backed by the given provider and loader.
func NewCoder(provider llm.Provider, loader *prompts.Loader) *Coder {
	return &Coder{provider: provider, loader: loader}
}

func (c *Coder) Role() string { return "coder" }

// coderData is the template context for coder.tmpl.
type coderData struct {
	Task           *models.Task
	PlanJSON       string
	ReviewFeedback string // empty on first pass
	Revision       int    // previous revision number (0 on first pass)
	NextRevision   int
}

func (c *Coder) Process(ctx context.Context, st *state.WorkflowState) error {
	st.SetStatus(models.StatusCoding)

	plan := st.GetPlan()
	if plan == nil {
		return fmt.Errorf("coder: no plan available in state")
	}

	planBytes, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("coder: marshal plan: %w", err)
	}

	review := st.LatestReview()
	feedback := ""
	prevRevision := 0
	if review != nil {
		feedback = buildFeedbackText(review)
		prevRevision = review.Revision
	}

	data := coderData{
		Task:           st.Task,
		PlanJSON:       string(planBytes),
		ReviewFeedback: feedback,
		Revision:       prevRevision,
		NextRevision:   prevRevision + 1,
	}

	sysPrompt, err := c.loader.Render("coder", data)
	if err != nil {
		return fmt.Errorf("coder: render prompt: %w", err)
	}

	userMsg := "Implement the project."
	if feedback != "" {
		userMsg = fmt.Sprintf("Revise the implementation based on the feedback above (revision %d).", prevRevision+1)
	}

	resp, err := c.provider.Generate(ctx, llm.Request{
		SystemPrompt: sysPrompt,
		Messages:     []llm.Message{{Role: "user", Content: userMsg}},
		MaxTokens:    8192,
		Temperature:  0.1,
	})
	if err != nil {
		return fmt.Errorf("coder: llm: %w", err)
	}

	raw := extractJSON(resp.Content)
	var artifact models.Artifact
	if err := json.Unmarshal([]byte(raw), &artifact); err != nil {
		return fmt.Errorf("coder: parse artifact JSON: %w\nraw response:\n%s", err, resp.Content)
	}

	st.SetArtifact(&artifact)
	st.AddLog("coder", fmt.Sprintf("artifact revision %d produced (%d files): %s",
		artifact.Revision, len(artifact.Files), artifact.Summary))
	return nil
}

// buildFeedbackText formats a Review into a readable string for the coder prompt.
func buildFeedbackText(r *models.Review) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Overall: %s\n\n", r.Feedback)
	if len(r.Issues) > 0 {
		b.WriteString("Issues to address:\n")
		for i, issue := range r.Issues {
			location := issue.File
			if location == "" {
				location = "general"
			}
			fmt.Fprintf(&b, "%d. [%s] %s (%s)\n   Fix: %s\n",
				i+1, issue.Severity, issue.Description, location, issue.Suggestion)
		}
	}
	return b.String()
}
