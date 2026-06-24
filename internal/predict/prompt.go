package predict

import (
	"fmt"
	"strings"

	"mugi/internal/index"
)

// systemData is the template context for swebench_diff.tmpl.
type systemData struct {
	Repo string
}

// userMessage assembles the user turn: the bug report followed by the retrieved
// code, each excerpt fenced with plain text markers (not markdown) and labelled
// with its repository-relative path and line range, so the model can target real
// paths in its diff. The context is bounded — at most MaxContextChunks excerpts,
// each truncated to MaxChunkRunes — so a pathological retrieval can't blow the
// prompt budget.
func userMessage(task Task, chunks []index.Chunk, cfg Config) string {
	var b strings.Builder
	b.WriteString("Bug report:\n\n")
	b.WriteString(strings.TrimSpace(task.ProblemStatement))
	b.WriteString("\n\n")

	if len(chunks) == 0 {
		b.WriteString("No repository excerpts were retrieved. Infer the fix from the report.\n")
		return b.String()
	}

	if len(chunks) > cfg.MaxContextChunks {
		chunks = chunks[:cfg.MaxContextChunks]
	}
	b.WriteString("Relevant code from the repository (most relevant first):\n")
	for _, c := range chunks {
		fmt.Fprintf(&b, "\n%s\nFile: %s  (lines %d-%d)\n%s\n%s\n",
			contextRule, c.Path, c.Start, c.End, contextRule, capRunes(c.Content, cfg.MaxChunkRunes))
	}
	b.WriteString("\nNow output the unified diff that fixes the bug.\n")
	return b.String()
}

// contextRule visually separates excerpts without using markdown code fences,
// which keeps the model from echoing ``` markers into its diff output.
const contextRule = "----------------------------------------"
