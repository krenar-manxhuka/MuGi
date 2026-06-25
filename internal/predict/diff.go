package predict

import (
	"fmt"
	"regexp"
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
	// Drop trailing blank lines *before* recounting — a spurious trailing context
	// line would otherwise inflate the hunk counts and then be trimmed away,
	// leaving the header disagreeing with the body again.
	s = dropTrailingBlankLines(s)
	if strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("no diff found in model output")
	}
	s = recountHunks(s)
	if err := ValidateDiff(s); err != nil {
		return "", err
	}
	// git apply is happiest with a trailing newline.
	return s + "\n", nil
}

// dropTrailingBlankLines removes whitespace-only lines from the end of the diff
// (e.g. a context blank line normalized to a single space). It leaves added or
// removed lines alone, since those carry a '+'/'-' prefix and aren't blank.
func dropTrailingBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return strings.Join(lines[:end], "\n")
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
			if ln == "" {
				// A blank context line must carry its leading space, or `patch`
				// rejects the whole hunk as "malformed patch". Models routinely
				// emit it as a bare empty line; normalize it.
				ln = " "
			}
			out = append(out, ln)
		default:
			return strings.Join(out, "\n")
		}
	}
	return strings.Join(out, "\n")
}

// hunkHeaderRe matches a unified-diff hunk header, capturing the old start, new
// start, and the trailing section text (` @@ def foo():`). The line counts are
// deliberately not captured — recountHunks recomputes them.
var hunkHeaderRe = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@(.*)$`)

// recountHunks rewrites each `@@` header so its line counts match the actual hunk
// body. Models frequently miscount — e.g. "@@ -237,10 +237,14 @@" for a hunk that
// really spans 12 old / 15 new lines — and `patch`/`git apply` then reject the
// hunk as malformed once the declared count runs out mid-body. The start line
// numbers are left as given (the patcher locates by context); only the counts are
// made truthful. Input must already be a clean diff (post scanDiff), so every
// body line begins with ' ', '+', '-', or '\'.
func recountHunks(diff string) string {
	lines := strings.Split(diff, "\n")
	i := 0
	for i < len(lines) {
		m := hunkHeaderRe.FindStringSubmatch(lines[i])
		if m == nil {
			i++
			continue
		}
		oldCount, newCount := 0, 0
		j := i + 1
		for j < len(lines) {
			ln := lines[j]
			if strings.HasPrefix(ln, "@@") || isDiffHeaderLine(ln) {
				break
			}
			switch {
			case ln == "", strings.HasPrefix(ln, " "):
				oldCount++
				newCount++
			case strings.HasPrefix(ln, "-"):
				oldCount++
			case strings.HasPrefix(ln, "+"):
				newCount++
			case strings.HasPrefix(ln, "\\"):
				// "\ No newline at end of file" — counts on neither side.
			}
			j++
		}
		lines[i] = fmt.Sprintf("@@ -%s,%d +%s,%d @@%s", m[1], oldCount, m[2], newCount, m[3])
		i = j
	}
	return strings.Join(lines, "\n")
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
