package swebench

import (
	"context"
	"strings"
)

// Outcome is the result of evaluating one candidate patch against one instance.
type Outcome struct {
	InstanceID string `json:"instance_id"`
	Resolved   bool   `json:"resolved"`

	// PatchApplied is false when the candidate patch did not apply cleanly — a
	// common, non-fatal outcome for model-generated diffs that simply scores as
	// unresolved (an empty candidate is treated as "applied", since there is
	// nothing to reject).
	PatchApplied bool `json:"patch_applied"`

	FailToPass map[string]Status `json:"fail_to_pass"`
	PassToPass map[string]Status `json:"pass_to_pass"`

	Log string `json:"log,omitempty"` // truncated test output, for debugging
	Err string `json:"error,omitempty"`
}

// EvalConfig parameterises a single evaluation.
type EvalConfig struct {
	// TestCmd is the command that runs the relevant tests in the checkout (e.g.
	// {"pytest", "-rA", "tests/test_x.py"} or {"go", "test", "./...", "-v"}). For
	// real SWE-bench this is derived per repo/version; here it is supplied
	// explicitly so the core stays framework-agnostic.
	TestCmd []string
	// Parse turns the test output into a status map (ParsePytest / ParseGoTest).
	Parse LogParser
	// MaxLog caps the stored log size in the Outcome (0 = a 16 KiB default).
	MaxLog int
}

// Evaluate runs the full held-out evaluation for one candidate patch:
//
//  1. apply the candidate patch (the fix under test; empty = empty-diff baseline),
//  2. apply the instance's test_patch (the held-out tests the agent never saw),
//  3. run the test command,
//  4. parse the output and score FAIL_TO_PASS / PASS_TO_PASS.
//
// The Environment is assumed to already be a clean checkout at the base commit;
// callers reuse it for at most one candidate. A patch that fails to apply is
// reported as PatchApplied=false and Resolved=false — not an error — because for
// model-generated diffs that is an ordinary result, not a harness fault.
func Evaluate(ctx context.Context, inst Instance, candidatePatch string, env Environment, cfg EvalConfig) Outcome {
	out := Outcome{
		InstanceID:   inst.InstanceID,
		PatchApplied: true,
		FailToPass:   map[string]Status{},
		PassToPass:   map[string]Status{},
	}

	if strings.TrimSpace(candidatePatch) != "" {
		if err := env.Apply(ctx, candidatePatch); err != nil {
			out.PatchApplied = false
			out.Err = "candidate patch did not apply: " + err.Error()
			return out // unresolved; nothing more to measure
		}
	}

	// The held-out tests must always apply; if they don't, the instance/dataset
	// is broken and the run is invalid (distinct from a model patch not applying).
	if err := env.Apply(ctx, inst.TestPatch); err != nil {
		out.Err = "test_patch did not apply (invalid instance/setup): " + err.Error()
		return out
	}

	output, err := env.Run(ctx, cfg.TestCmd)
	out.Log = truncate(output, cfg.maxLog())
	if err != nil {
		out.Err = "running tests: " + err.Error()
		return out
	}

	statuses := cfg.Parse(output)
	score := ScoreInstance(inst, statuses)
	out.Resolved = score.Resolved
	out.FailToPass = score.FailToPass
	out.PassToPass = score.PassToPass
	return out
}

func (c EvalConfig) maxLog() int {
	if c.MaxLog <= 0 {
		return 16 * 1024
	}
	return c.MaxLog
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n...[truncated]"
}
