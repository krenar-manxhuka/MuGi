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
