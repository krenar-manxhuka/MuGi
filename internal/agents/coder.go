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

// coderProfile is the Coder's generation contract. It needs the largest token
// budget because it emits multi-file source artifacts; smaller caps truncated the
// hardest tasks (e.g. expr-eval) into invalid JSON. max_tokens is a ceiling, not
// a charge, and haiku/sonnet allow 64K output, so the headroom is free.
var coderProfile = generationProfile{
	role:        "coder",
	maxTokens:   16384,
	temperature: 0.1,
	attempts:    2,
}

// Coder implements the plan produced by the Planner and revises its output
// based on feedback from the Reviewer.
type Coder struct {
	llmAgent
}

// NewCoder returns a Coder backed by the given provider and loader.
func NewCoder(provider llm.Provider, loader *prompts.Loader) *Coder {
	return &Coder{llmAgent{provider: provider, loader: loader}}
}

func (c *Coder) Role() string { return coderProfile.role }

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
	// As an extra safeguard: the plan was validated when produced, but it reached us
	// through mutable shared state — re-check before building on it.
	if err := plan.Validate(); err != nil {
		return fmt.Errorf("coder: invalid plan in state: %w", err)
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

	artifact, err := generateFor[models.Artifact, *models.Artifact](ctx, c.llmAgent, coderProfile, sysPrompt, userMsg)
	if err != nil {
		return err
	}

	// Canonicalise Go sources before they are built, reviewed, or written out.
	formatGoSources(artifact)

	st.SetArtifact(artifact)
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
