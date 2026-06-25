package predict

import (
	"strings"
	"testing"
)

func TestExtractDiff(t *testing.T) {
	body := "--- a/x.py\n+++ b/x.py\n@@ -1 +1 @@\n-a\n+b"
	cases := []struct {
		name string
		in   string
		want bool // expect success
	}{
		{"raw", body, true},
		{"git-header", "diff --git a/x.py b/x.py\n" + body, true},
		{"fenced diff", "```diff\n" + body + "\n```", true},
		{"fenced no-lang", "```\n" + body + "\n```", true},
		{"leading prose", "Here is the fix:\n\n" + body, true},
		{"prose then fence", "Sure!\n```diff\n" + body + "\n```\nDone.", true},
		{"empty", "", false},
		{"prose only", "I cannot determine the fix.", false},
		{"headers no hunk", "--- a/x.py\n+++ b/x.py\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ExtractDiff(tc.in)
			if tc.want && err != nil {
				t.Fatalf("expected success, got error: %v", err)
			}
			if !tc.want {
				if err == nil {
					t.Fatalf("expected error, got diff:\n%q", got)
				}
				return
			}
			if !strings.HasSuffix(got, "\n") {
				t.Errorf("extracted diff should end with a newline for git apply")
			}
			if strings.Contains(got, "```") {
				t.Errorf("fences were not stripped:\n%q", got)
			}
			if err := ValidateDiff(strings.TrimRight(got, "\n")); err != nil {
				t.Errorf("extracted diff fails validation: %v", err)
			}
		})
	}
}

// The model (esp. Haiku) emits a diff, then a </diff> tag, prose, and a second
// diff. These are the real failure shapes from the first SWE-bench run: the patch
// must be cut at the first non-diff line or git apply rejects the whole thing.
func TestExtractDiff_stopsAtTagsAndProse(t *testing.T) {
	firstHunk := "--- a/x.py\n+++ b/x.py\n@@ -1,2 +1,2 @@\n a\n-b\n+c"
	contaminated := firstHunk + "\n</diff>\n\nWait, let me reconsider. Actually:\n\n" +
		"<diff --git a/x.py b/x.py\n--- a/x.py\n+++ b/x.py\n@@ -1,2 +1,2 @@\n a\n-b\n+WRONG\n</diff>"

	got, err := ExtractDiff(contaminated)
	if err != nil {
		t.Fatalf("ExtractDiff: %v", err)
	}
	for _, bad := range []string{"</diff>", "Wait", "reconsider", "+WRONG"} {
		if strings.Contains(got, bad) {
			t.Errorf("extracted diff still contains %q:\n%s", bad, got)
		}
	}
	if !strings.Contains(got, "+c") {
		t.Errorf("extracted diff lost the real change (+c):\n%s", got)
	}
}

func TestExtractDiff_trailingTagOnly(t *testing.T) {
	// The shape that *happened* to apply on two instances — strip the tag anyway.
	got, err := ExtractDiff("--- a/x.py\n+++ b/x.py\n@@ -1 +1 @@\n-a\n+b\n</diff>")
	if err != nil {
		t.Fatalf("ExtractDiff: %v", err)
	}
	if strings.Contains(got, "</diff>") {
		t.Errorf("trailing tag not stripped:\n%s", got)
	}
}

func TestExtractDiff_blankContextLineGetsLeadingSpace(t *testing.T) {
	// A blank context line emitted bare-empty (no leading space) makes `patch`
	// reject the hunk as "malformed patch" — it must be normalized to " ".
	got, err := ExtractDiff("--- a/x.py\n+++ b/x.py\n@@ -1,4 +1,4 @@\n a\n\n-b\n+c")
	if err != nil {
		t.Fatalf("ExtractDiff: %v", err)
	}
	inHunk := false
	for _, ln := range strings.Split(strings.TrimRight(got, "\n"), "\n") {
		if strings.HasPrefix(ln, "@@") {
			inHunk = true
			continue
		}
		if inHunk && ln == "" {
			t.Errorf("bare empty hunk line not normalized:\n%q", got)
		}
	}
	if !strings.Contains(got, "\n \n") {
		t.Errorf("expected a space-only context line in:\n%q", got)
	}
}

func TestExtractDiff_recountsWrongHunkHeader(t *testing.T) {
	// Header claims 1 old / 1 new, but the body is 3 old / 3 new. The wrong count
	// is what makes patch reject the hunk; recount must correct it.
	got, err := ExtractDiff("--- a/x.py\n+++ b/x.py\n@@ -5,1 +5,1 @@ def f():\n a\n-b\n-c\n+d\n+e")
	if err != nil {
		t.Fatalf("ExtractDiff: %v", err)
	}
	if !strings.Contains(got, "@@ -5,3 +5,3 @@ def f():") {
		t.Errorf("hunk header not recounted (want -5,3 +5,3):\n%s", got)
	}
}

func TestExtractDiff_keepsMultiFilePatch(t *testing.T) {
	// A legitimate two-file patch (consecutive headers, no prose) must survive.
	two := "diff --git a/x.py b/x.py\n--- a/x.py\n+++ b/x.py\n@@ -1 +1 @@\n-a\n+b\n" +
		"diff --git a/y.py b/y.py\n--- a/y.py\n+++ b/y.py\n@@ -1 +1 @@\n-c\n+d"
	got, err := ExtractDiff(two)
	if err != nil {
		t.Fatalf("ExtractDiff: %v", err)
	}
	if !strings.Contains(got, "+b") || !strings.Contains(got, "+d") {
		t.Errorf("multi-file patch was truncated:\n%s", got)
	}
}

func TestValidateDiff(t *testing.T) {
	if err := ValidateDiff("--- a/x\n+++ b/x\n@@ -1 +1 @@\n-a\n+b"); err != nil {
		t.Errorf("a well-formed diff should validate: %v", err)
	}
	if err := ValidateDiff("diff --git a/x b/x\n@@ -1 +1 @@\n-a\n+b"); err != nil {
		t.Errorf("a git-header diff should validate: %v", err)
	}
	if err := ValidateDiff("just some text"); err == nil {
		t.Errorf("non-diff text should fail validation")
	}
	if err := ValidateDiff("--- a/x\n+++ b/x\n"); err == nil {
		t.Errorf("headers without a hunk should fail validation")
	}
}
