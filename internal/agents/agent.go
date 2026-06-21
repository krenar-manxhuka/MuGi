// Package agents contains the four specialised agents and their shared abstraction.
package agents

import (
	"context"
	"fmt"
	"strings"

	"mugi/internal/llm"
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

// truncationHint returns a clarifying suffix when the response was cut off at
// the MaxTokens ceiling. That truncation is the usual cause of an "unexpected
// end of JSON input" error when parsing an agent's structured output, so we
// surface it explicitly rather than leaving a cryptic parse error.
func truncationHint(resp llm.Response) string {
	if resp.Truncated() {
		return " [LLM output was truncated at the MaxTokens limit — raise MaxTokens for this agent]"
	}
	return ""
}

// extractJSON normalises an LLM response into parseable JSON. It strips a
// Markdown code fence (real models often wrap output in ```json ... ``` even
// when instructed not to) and then repairs raw control characters that some
// models emit unescaped inside string literals (see repairJSONStrings).
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		// Drop the opening fence line (```json or just ```)
		if nl := strings.IndexByte(s, '\n'); nl != -1 {
			s = s[nl+1:]
			// Drop the closing fence
			if idx := strings.LastIndex(s, "```"); idx != -1 {
				s = s[:idx]
			}
			s = strings.TrimSpace(s)
		}
	}
	return repairJSONStrings(s)
}

// repairJSONStrings escapes raw control characters (newline, tab, carriage
// return, and other bytes < 0x20) that appear unescaped *inside* JSON string
// literals. Weaker models routinely paste multi-line source code straight into
// a "content" field without escaping it, which is a hard json.Unmarshal error
// even though the intent is unambiguous. We only touch bytes inside strings, so
// already-valid JSON passes through byte-for-byte unchanged.
//
// It does not attempt to fix unescaped double quotes inside strings: there is
// no unambiguous way to tell a stray inner quote from a legitimate string
// terminator, so guessing risks corrupting otherwise-valid input.
func repairJSONStrings(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inString := false
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !inString {
			if c == '"' {
				inString = true
			}
			b.WriteByte(c)
			continue
		}
		if escaped {
			b.WriteByte(c)
			escaped = false
			continue
		}
		switch c {
		case '\\':
			b.WriteByte(c)
			escaped = true
		case '"':
			b.WriteByte(c)
			inString = false
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if c < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, c)
			} else {
				b.WriteByte(c)
			}
		}
	}
	return b.String()
}
