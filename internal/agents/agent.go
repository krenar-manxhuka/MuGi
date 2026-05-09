// Package agents contains the four specialised agents and their shared abstraction.
package agents

import (
	"context"
	"strings"

	"mugi/internal/state"
)

// Agent processes a step of the workflow by reading from and writing to the
// shared WorkflowState. Agent implementations must be safe for sequential use
// (they are never called concurrently on the same state).
type Agent interface {
	// Role returns the agent's name, e.g. "planner".
	Role() string

	// Process executes the agent's logic for the current workflow state.
	// Implementations should update state before returning.
	Process(ctx context.Context, st *state.WorkflowState) error
}

// extractJSON strips a Markdown code fence from an LLM response, returning the
// raw JSON content. Real models often wrap output in ```json ... ``` even when
// instructed not to; this makes parsing resilient to that habit.
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	// Drop the opening fence line (```json or just ```)
	nl := strings.IndexByte(s, '\n')
	if nl == -1 {
		return s
	}
	s = s[nl+1:]
	// Drop the closing fence
	if idx := strings.LastIndex(s, "```"); idx != -1 {
		s = s[:idx]
	}
	return strings.TrimSpace(s)
}
