package agents

import (
	"context"
	"encoding/json"
	"fmt"

	"mugi/internal/llm"
)

// validatable is implemented by domain objects that can check their own invariants.
type validatable interface {
	Validate() error
}

// generateStructured is the single, shared LLM→domain boundary. It runs gen to
// obtain a response, extracts and parses JSON into a fresh *T, then validates the
// result's invariants. This is the keystone that prevents malformed or
// semantically-empty model output from reaching the rest of the pipeline.
//
// Retry policy: on a *recoverable* failure (malformed JSON or a failed invariant)
// it resamples up to `attempts` times — a fresh, slightly different sample usually
// parses. The one exception is a response truncated at the token ceiling:
// resampling with the same limit would just truncate again, so it stops early and
// surfaces a clear "raise MaxTokens" hint instead of burning attempts.
//
// label names the agent for error messages (e.g. "planner").
//
// The PT constraint says "the pointer to T validates itself" — Validate has a
// pointer receiver on Plan/Artifact/Review.
func generateStructured[T any, PT interface {
	*T
	validatable
}](
	ctx context.Context,
	label string,
	attempts int,
	gen func(context.Context) (llm.Response, error),
) (*T, error) {
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		resp, err := gen(ctx)
		if err != nil {
			return nil, fmt.Errorf("%s: llm: %w", label, err)
		}

		var v T
		raw := extractJSON(resp.Content)
		if uerr := json.Unmarshal([]byte(raw), &v); uerr != nil {
			lastErr = fmt.Errorf("%s: parse JSON: %w%s", label, uerr, truncationHint(resp))
		} else if verr := PT(&v).Validate(); verr != nil {
			lastErr = fmt.Errorf("%s: invalid output: %w%s", label, verr, truncationHint(resp))
		} else {
			return &v, nil
		}

		// A resample cannot un-truncate a response — stop wasting (often expensive) calls.
		if resp.Truncated() {
			break
		}
	}
	return nil, lastErr
}
