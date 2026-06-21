package agents

import (
	"context"
	"fmt"

	"mugi/internal/llm"
	"mugi/internal/prompts"
)

// llmAgent is the shared substrate for every agent backed by an LLM and prompt
// templates. Embedding it keeps the provider/loader wiring in exactly one place
// (DRY) and gives all agents an identical construction shape.
type llmAgent struct {
	provider llm.Provider
	loader   *prompts.Loader
}

// generationProfile is an agent's LLM generation contract, expressed as a named
// value object rather than magic numbers scattered across call sites:
//
//   - maxTokens   — the output ceiling the agent needs (too small silently
//     truncates structured output into invalid JSON — the exact bug this design
//     is meant to make impossible to reintroduce unnoticed).
//   - temperature — how deterministic the output should be.
//   - attempts    — how many times to resample on a recoverable failure.
//
// Encoding the token-economy and retry policy as a profile makes them explicit,
// reviewable, and validated (see validate) instead of incidental.
type generationProfile struct {
	role        string
	maxTokens   int
	temperature float64
	attempts    int
}

// validate guards against a misconfigured profile. A zero token budget or zero
// attempts would silently break an agent, so we fail fast and loudly at the
// boundary (by construction: an object is never used in an invalid state).
func (p generationProfile) validate() error {
	switch {
	case p.role == "":
		return fmt.Errorf("generation profile: empty role")
	case p.maxTokens <= 0:
		return fmt.Errorf("generation profile %q: maxTokens must be > 0, got %d", p.role, p.maxTokens)
	case p.attempts < 1:
		return fmt.Errorf("generation profile %q: attempts must be >= 1, got %d", p.role, p.attempts)
	}
	return nil
}

// generateFor is the single place a structured agent issues an LLM request. It
// builds one user turn from the agent's profile and routes the response through
// the shared validation boundary (generateStructured: parse → Validate → retry).
// Centralising request construction means the token-economy and retry policy can
// never drift between agents.
func generateFor[T any, PT interface {
	*T
	validatable
}](
	ctx context.Context,
	a llmAgent,
	profile generationProfile,
	systemPrompt, userMsg string,
) (*T, error) {
	if err := profile.validate(); err != nil {
		return nil, err
	}
	return generateStructured[T, PT](ctx, profile.role, profile.attempts,
		func(ctx context.Context) (llm.Response, error) {
			return a.provider.Generate(ctx, llm.Request{
				SystemPrompt: systemPrompt,
				Messages:     []llm.Message{{Role: "user", Content: userMsg}},
				MaxTokens:    profile.maxTokens,
				Temperature:  profile.temperature,
			})
		})
}
