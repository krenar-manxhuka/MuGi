package predict

import (
	"fmt"
	"strings"
)

// ExtractDiff pulls a unified diff out of a model reply and sanity-checks it.
// Models wrap diffs inconsistently — raw, inside a ```diff fence, behind a line of
// prose, or (especially smaller models) as a diff followed by a `</diff>` tag,
// some "wait, let me reconsider…" commentary, and a *second* diff. So this unwraps
// a fenced block, drops anything before the first diff header, and then keeps only
// the first well-formed diff, cutting at the first line that can't belong to it —
// because a trailing `</diff>` or prose line makes `git apply` reject the whole
// patch. It is a guard, not a full parser: the harness's `git apply` is the real
// arbiter of whether a patch applies.
func ExtractDiff(raw string) (string, error) {
	s := strings.ReplaceAll(raw, "\r\n", "\n")
	if inner, ok := fencedBlock(s); ok {
		s = inner
	}
	s = sliceFromDiffStart(s)
	s = scanDiff(s)
	s = strings.TrimRight(s, " \t\n")
	if strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("no diff found in model output")
	}
	if err := ValidateDiff(s); err != nil {
		return "", err
	}
	// git apply is happiest with a trailing newline.
	return s + "\n", nil
}

// scanDiff keeps only the leading well-formed unified diff, stopping at the first
// line that cannot be part of one. The input must already start at a diff header
// (see sliceFromDiffStart). Header lines and `@@` hunks are kept; once inside a
// hunk, only context/added/removed/"\ No newline" lines (and blank lines a model
// forgot to prefix) belong — anything else (a `</diff>` tag, a prose sentence, a
// `<diff` re-opener) ends the diff. Consecutive `diff --git`/`---` headers with no
// prose between them keep multi-file patches intact.
func scanDiff(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	inHunk := false
	for _, ln := range lines {
		switch {
		case strings.HasPrefix(ln, "@@"):
			inHunk = true
			out = append(out, ln)
		case isDiffHeaderLine(ln):
			inHunk = false
			out = append(out, ln)
		case inHunk && isHunkBodyLine(ln):
			out = append(out, ln)
		default:
			return strings.Join(out, "\n")
		}
	}
	return strings.Join(out, "\n")
}

// diffHeaderPrefixes are the line starts of a git/unified diff's file-header
// section (the lines before each `@@` hunk).
var diffHeaderPrefixes = []string{
	"diff --git ", "index ", "--- ", "+++ ",
	"new file mode", "deleted file mode", "old mode ", "new mode ",
	"rename from ", "rename to ", "copy from ", "copy to ",
	"similarity index ", "dissimilarity index ",
	"Binary files ", "GIT binary patch",
}

func isDiffHeaderLine(ln string) bool {
	for _, p := range diffHeaderPrefixes {
		if strings.HasPrefix(ln, p) {
			return true
		}
	}
	return false
}

// isHunkBodyLine reports whether ln can appear inside a hunk: a context (' '),
// added ('+'), removed ('-'), or "\ No newline at end of file" ('\\') line. An
// empty line is allowed too — models often emit a blank context line without the
// leading space.
func isHunkBodyLine(ln string) bool {
	if ln == "" {
		return true
	}
	switch ln[0] {
	case ' ', '+', '-', '\\':
		return true
	}
	return false
}

// ValidateDiff reports whether diff has the minimal shape of a unified diff: file
// headers (either a `diff --git` line or a `--- `/`+++ ` pair) and at least one
// `@@` hunk. It does not attempt to apply it.
func ValidateDiff(diff string) error {
	hasGit := strings.Contains(diff, "diff --git ")
	hasMinus := strings.HasPrefix(diff, "--- ") || strings.Contains(diff, "\n--- ")
	hasPlus := strings.HasPrefix(diff, "+++ ") || strings.Contains(diff, "\n+++ ")
	if !hasGit && !(hasMinus && hasPlus) {
		return fmt.Errorf("output is not a unified diff (missing file headers)")
	}
	if !strings.Contains(diff, "@@") {
		return fmt.Errorf("output is not a unified diff (no @@ hunk header)")
	}
	return nil
}

// fencedBlock returns the contents of the first ``` fenced block if that block
// looks like a diff, so a model that wraps its answer in ```diff … ``` is handled.
// It returns ok=false when there is no diff-looking fence, leaving the caller to
// treat the whole text as the candidate.
func fencedBlock(s string) (string, bool) {
	const fence = "```"
	i := strings.Index(s, fence)
	if i < 0 {
		return "", false
	}
	rest := s[i+len(fence):]
	// Skip the remainder of the opening fence line (e.g. the "diff" in ```diff).
	nl := strings.IndexByte(rest, '\n')
	if nl < 0 {
		return "", false
	}
	rest = rest[nl+1:]
	if j := strings.Index(rest, fence); j >= 0 {
		rest = rest[:j]
	}
	if !looksLikeDiff(rest) {
		return "", false
	}
	return rest, true
}

// sliceFromDiffStart drops any leading prose, returning the text from the first
// diff header line onward.
func sliceFromDiffStart(s string) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		if strings.HasPrefix(ln, "diff --git ") || strings.HasPrefix(ln, "--- ") {
			return strings.Join(lines[i:], "\n")
		}
	}
	return s
}

func looksLikeDiff(s string) bool {
	return strings.Contains(s, "diff --git ") ||
		strings.Contains(s, "--- ") ||
		strings.Contains(s, "@@")
}
