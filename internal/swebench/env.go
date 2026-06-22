package swebench

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Environment is a prepared repository checkout that an evaluation applies
// patches to and runs commands in. It is the seam between the pure eval logic
// and *where* execution actually happens:
//
//   - LocalEnv (this file) runs commands directly in a temp directory — used by
//     the offline reference tests, where the "repo" is a small local fixture.
//   - A future DockerEnv will run the same Apply/Run calls inside a per-instance
//     SWE-bench container on a CI runner. The eval flow in Evaluate does not
//     change; only the Environment does.
type Environment interface {
	// Apply applies a unified-diff patch to the checkout. Implementations must
	// report an error if the patch does not apply cleanly.
	Apply(ctx context.Context, patch string) error
	// Run executes a command in the checkout and returns its combined output.
	// A non-zero exit is NOT an error here: test runners exit non-zero when tests
	// fail, and that output is exactly what the parser needs. err is reserved for
	// failures to launch the command at all.
	Run(ctx context.Context, args []string) (string, error)
}

// LocalEnv runs commands directly in Dir, an existing checkout of the repo at
// the instance's base commit. It applies patches with `git apply`, so Dir must
// be a git work tree.
type LocalEnv struct {
	Dir string
}

// NewLocalEnv wraps an existing checkout directory.
func NewLocalEnv(dir string) *LocalEnv { return &LocalEnv{Dir: dir} }

// Apply pipes the patch into `git apply` so model- or dataset-supplied diffs are
// applied exactly as git would, including new-file and rename hunks.
func (e *LocalEnv) Apply(ctx context.Context, patch string) error {
	if strings.TrimSpace(patch) == "" {
		return nil
	}
	cmd := exec.CommandContext(ctx, "git", "apply", "--verbose", "-")
	cmd.Dir = e.Dir
	cmd.Stdin = strings.NewReader(patch)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git apply: %w\n%s", err, strings.TrimSpace(buf.String()))
	}
	return nil
}

// Run executes args[0] with args[1:] in Dir and returns combined output.
func (e *LocalEnv) Run(ctx context.Context, args []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("run: empty command")
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = e.Dir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := buf.String()
	// Distinguish "command failed to launch" from "command ran and exited non-zero"
	// (a normal test failure): only the former is an error for our purposes.
	if err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			return out, fmt.Errorf("run %v: %w", args, err)
		}
	}
	return out, nil
}
