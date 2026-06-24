package predict

import (
	"fmt"
	"strings"
)

// ExtractDiff pulls a unified diff out of a model reply and sanity-checks it.
// Models wrap diffs inconsistently — raw, inside a ```diff fence, or after a line
// of prose — so this unwraps a fenced block when present, drops anything before
// the first diff header, and validates the result has the shape git apply needs.
// It is a guard, not a full parser: the official harness's `git apply` is the
// real arbiter of whether a patch applies.
func ExtractDiff(raw string) (string, error) {
	s := strings.ReplaceAll(raw, "\r\n", "\n")
	if inner, ok := fencedBlock(s); ok {
		s = inner
	}
	s = sliceFromDiffStart(s)
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
