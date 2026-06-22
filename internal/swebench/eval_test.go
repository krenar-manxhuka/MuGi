package swebench

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestEvaluate_GoldEmptyBaselines exercises the whole eval flow offline against a
// tiny local git repo — the same gold-patch / empty-patch baselines the real
// SWE-bench run uses, but with `go test` instead of pytest so it needs no Docker,
// no Python, and no network. It proves:
//
//   - the gold patch resolves the instance (FAIL_TO_PASS flips to passing),
//   - the empty-diff baseline does NOT (the held-out test still fails),
//   - a non-applying patch is reported as PatchApplied=false, not resolved.
func TestEvaluate_GoldEmptyBaselines(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not available")
	}

	dir := t.TempDir()
	gitC := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Base state: Add is buggy (subtracts), committed at the "base commit".
	gitC("init")
	gitC("config", "user.email", "test@example.com")
	gitC("config", "user.name", "Test")
	write("go.mod", "module example.com/buggy\n\ngo 1.21\n")
	write("math.go", "package buggy\n\nfunc Add(a, b int) int { return a - b }\n")
	gitC("add", ".")
	gitC("commit", "-m", "base")

	// Gold patch = the fix, captured as a real git diff.
	write("math.go", "package buggy\n\nfunc Add(a, b int) int { return a + b }\n")
	goldPatch := gitC("diff")
	gitC("checkout", "--", "math.go")

	// Test patch = the held-out test, captured as a new-file git diff.
	testFile := "package buggy\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(2, 3) != 5 {\n\t\tt.Fatalf(\"Add(2,3) = %d, want 5\", Add(2, 3))\n\t}\n}\n"
	write("math_test.go", testFile)
	gitC("add", "-N", "math_test.go")
	testPatch := gitC("diff")
	gitC("reset")
	_ = os.Remove(filepath.Join(dir, "math_test.go"))

	reset := func() {
		gitC("checkout", "--", ".")
		gitC("clean", "-fd")
	}

	inst := Instance{
		InstanceID: "buggy__add-1",
		Repo:       "example.com/buggy",
		BaseCommit: "HEAD",
		TestPatch:  testPatch,
		Patch:      goldPatch,
		FailToPass: StringList{"TestAdd"},
	}
	cfg := EvalConfig{TestCmd: []string{"go", "test", "./...", "-v"}, Parse: ParseGoTest}
	env := NewLocalEnv(dir)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// Gold patch must resolve.
	reset()
	gold := Evaluate(ctx, inst, inst.Patch, env, cfg)
	if gold.Err != "" {
		t.Fatalf("gold eval error: %s\nlog:\n%s", gold.Err, gold.Log)
	}
	if !gold.PatchApplied {
		t.Fatal("gold patch should apply cleanly")
	}
	if !gold.Resolved {
		t.Fatalf("gold patch should RESOLVE the instance; FAIL_TO_PASS=%v\nlog:\n%s", gold.FailToPass, gold.Log)
	}

	// Empty-diff baseline must NOT resolve (held-out test still fails on buggy code).
	reset()
	empty := Evaluate(ctx, inst, "", env, cfg)
	if !empty.PatchApplied {
		t.Fatal("empty candidate is treated as applied")
	}
	if empty.Resolved {
		t.Fatal("empty-diff baseline must NOT resolve")
	}
	if empty.FailToPass["TestAdd"] != StatusFailed {
		t.Fatalf("empty baseline: TestAdd = %q, want FAILED\nlog:\n%s", empty.FailToPass["TestAdd"], empty.Log)
	}

	// A garbage patch must be reported as not applied (and unresolved).
	reset()
	bad := Evaluate(ctx, inst, "this is not a unified diff\n", env, cfg)
	if bad.PatchApplied {
		t.Fatal("garbage patch should not apply")
	}
	if bad.Resolved {
		t.Fatal("non-applying patch must be unresolved")
	}
}
