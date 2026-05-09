// Package runner compiles and tests a generated artifact in a temporary directory.
package runner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"mugi/internal/models"
)

// Run writes the artifact files to a temp directory, then compiles and (if the
// build succeeds) tests them. Returns an ExecResult with all captured output.
// A timeout of 0 uses the default of 60 seconds.
func Run(ctx context.Context, artifact *models.Artifact, timeout time.Duration) *models.ExecResult {
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	lang := detectLang(artifact.Files)
	switch lang {
	case "go":
		return runGo(ctx, artifact.Files, timeout)
	default:
		return &models.ExecResult{Lang: lang, Skipped: true}
	}
}

func detectLang(files []models.File) string {
	for _, f := range files {
		if f.Lang != "" {
			return strings.ToLower(f.Lang)
		}
	}
	return ""
}

func runGo(ctx context.Context, files []models.File, timeout time.Duration) *models.ExecResult {
	res := &models.ExecResult{Lang: "go"}

	dir, err := os.MkdirTemp("", "mugi-run-*")
	if err != nil {
		res.BuildOut = fmt.Sprintf("runner: create temp dir: %v", err)
		return res
	}
	defer os.RemoveAll(dir)

	// Write all files, creating subdirectories as needed.
	hasGoMod := false
	for _, f := range files {
		dest := filepath.Join(dir, filepath.FromSlash(f.Path))
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
	}

	// Create a minimal go.mod if the coder didn't produce one.
	if !hasGoMod {
		gomod := "module generated\n\ngo 1.21\n"
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
			res.BuildOut = fmt.Sprintf("runner: write go.mod: %v", err)
			return res
		}
		// Fetch missing dependencies so the build can actually succeed.
		tidyOut, _ := runCmd(ctx, dir, timeout, "go", "mod", "tidy")
		if tidyOut != "" {
			res.BuildOut += "[go mod tidy]\n" + tidyOut + "\n"
		}
	}

	// Build.
	buildOut, buildOK := runCmd(ctx, dir, timeout, "go", "build", "./...")
	res.BuildOK = buildOK
	res.BuildOut += buildOut

	// Test only when the build passed.
	if buildOK {
		testOut, testOK := runCmd(ctx, dir, timeout, "go", "test", "./...")
		res.TestOK = testOK
		res.TestOut = testOut
	}

	return res
}

// runCmd executes a command in dir, returning combined output and whether it exited 0.
func runCmd(ctx context.Context, dir string, timeout time.Duration, name string, args ...string) (string, bool) {
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(tctx, name, args...)
	cmd.Dir = dir

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	out := strings.TrimSpace(buf.String())
	if out == "" && err == nil {
		out = "ok"
	}
	return out, err == nil
}
