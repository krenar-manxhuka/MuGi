package retrievaleval

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestGitRepoSource_checksOutLocalRepo drives the real git path — init, fetch a
// specific commit, detached checkout — against a repository on the local
// filesystem, so it needs git but no network. It proves Checkout materializes the
// right commit and that the result feeds straight into the recall measurement.
func TestGitRepoSource_checksOutLocalRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	ctx := context.Background()

	// Build a source repo at <root>/acme/calc.git so that, with BaseURL=<root>/,
	// the source's URL (BaseURL + "acme/calc" + ".git") resolves to it.
	root := t.TempDir()
	repoFS := filepath.Join(root, "acme", "calc.git")
	if err := os.MkdirAll(repoFS, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoFS, "init", "--quiet", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(repoFS, "calc.py"),
		[]byte("def divide_numbers(n, d):\n    return n / d\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoFS, "add", "calc.py")
	runGit(t, repoFS,
		"-c", "user.email=t@example.com", "-c", "user.name=t", "-c", "commit.gpgsign=false",
		"commit", "--quiet", "-m", "add calc")
	sha := runGit(t, repoFS, "rev-parse", "HEAD")

	src := GitRepoSource{BaseURL: filepath.ToSlash(root) + "/"}
	dir, cleanup, err := src.Checkout(ctx, "acme/calc", sha)
	if err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	defer cleanup()

	if _, err := os.Stat(filepath.Join(dir, "calc.py")); err != nil {
		t.Fatalf("checked-out repo is missing calc.py: %v", err)
	}

	task := Task{
		ID:               "local",
		ProblemStatement: "divide_numbers divides n by d",
		GoldPatch:        goldPatchFor("calc.py"),
	}
	results, err := EvaluateTask(ctx, dir, task, Config{}, lexicalSpec(t), []int{5})
	if err != nil {
		t.Fatalf("EvaluateTask on checkout: %v", err)
	}
	if results[0].RecallAtK != 1.0 {
		t.Errorf("recall on local checkout = %.3f, want 1.0", results[0].RecallAtK)
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return trimNL(string(out))
}

func trimNL(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
