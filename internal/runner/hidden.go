package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mugi/internal/fsafe"
	"mugi/internal/models"
)

// RunHidden scores an artifact against a harness-authored test the coder never
// saw — the held-out acceptance signal. It is deliberately independent of
// whatever tests the model wrote for itself: it writes only the artifact's
// NON-test source (so the model's own, possibly weak or broken, tests cannot
// interfere), injects the hidden test into the implementation's own package,
// then builds and tests.
//
// hiddenBody is a Go test file WITHOUT a package clause (imports + Test
// functions); RunHidden prepends `package <detected>` so the test compiles in
// the same package as the implementation, whether that is `main` or a named
// library package. A timeout of 0 uses the default of 60 seconds.
func RunHidden(ctx context.Context, artifact *models.Artifact, hiddenBody string, timeout time.Duration) *models.ExecResult {
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	if artifact == nil {
		return &models.ExecResult{BuildOut: "runner: nil artifact"}
	}
	if detectLang(artifact.Files) != "go" {
		return &models.ExecResult{Lang: detectLang(artifact.Files), Skipped: true}
	}

	res := &models.ExecResult{Lang: "go"}

	dir, err := os.MkdirTemp("", "mugi-hidden-*")
	if err != nil {
		res.BuildOut = fmt.Sprintf("runner: create temp dir: %v", err)
		return res
	}
	defer os.RemoveAll(dir)

	pkg := "main"
	hasGoMod := false
	wroteSource := false
	for _, f := range artifact.Files {
		// Exclude the model's own tests: the hidden signal must come from the
		// implementation alone, not from tests the model graded itself on.
		if strings.HasSuffix(f.Path, "_test.go") {
			continue
		}
		dest, err := fsafe.SafeJoin(dir, f.Path)
		if err != nil {
			res.BuildOut = fmt.Sprintf("runner: unsafe file path %q: %v", f.Path, err)
			return res
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			res.BuildOut = fmt.Sprintf("runner: mkdir %s: %v", filepath.Dir(dest), err)
			return res
		}
		if err := os.WriteFile(dest, []byte(f.Content), 0o644); err != nil {
			res.BuildOut = fmt.Sprintf("runner: write %s: %v", f.Path, err)
			return res
		}
		if f.Path == "go.mod" {
			hasGoMod = true
		}
		if strings.HasSuffix(f.Path, ".go") {
			wroteSource = true
			if p := parsePackageName(f.Content); p != "" {
				pkg = p
			}
		}
	}

	if !wroteSource {
		res.BuildOut = "runner: artifact has no non-test Go source to test against"
		return res
	}

	if !hasGoMod {
		gomod := "module generated\n\ngo 1.21\n"
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
			res.BuildOut = fmt.Sprintf("runner: write go.mod: %v", err)
			return res
		}
		tidyOut, _ := runCmd(ctx, dir, timeout, "go", "mod", "tidy")
		if tidyOut != "" {
			res.BuildOut += "[go mod tidy]\n" + tidyOut + "\n"
		}
	}

	// Inject the hidden test into the implementation's package. The leading "zz_"
	// keeps it lexically last and unlikely to collide with a real source name.
	hidden := "package " + pkg + "\n\n" + strings.TrimLeft(hiddenBody, "\n")
	if err := os.WriteFile(filepath.Join(dir, "zz_hidden_test.go"), []byte(hidden), 0o644); err != nil {
		res.BuildOut = fmt.Sprintf("runner: write hidden test: %v", err)
		return res
	}

	buildOut, buildOK := runCmd(ctx, dir, timeout, "go", "build", "./...")
	res.BuildOK = buildOK
	res.BuildOut += buildOut

	if buildOK {
		testOut, testOK := runCmd(ctx, dir, timeout, "go", "test", "./...")
		res.TestOK = testOK
		res.TestOut = testOut
	}

	return res
}

// parsePackageName returns the package name declared by a Go source file, or ""
// if no package clause is found. It scans line by line and skips comments, which
// is enough for well-formed generated source (we do not need a full parser).
func parsePackageName(src string) string {
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "/*") || strings.HasPrefix(line, "*") {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "package "); ok {
			name := strings.TrimSpace(rest)
			// Drop any trailing line comment, e.g. "package foo // bar".
			if i := strings.Index(name, "//"); i != -1 {
				name = strings.TrimSpace(name[:i])
			}
			if f := strings.Fields(name); len(f) > 0 {
				return f[0]
			}
		}
	}
	return ""
}
