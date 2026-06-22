package swebench

import "strings"

// Status is the outcome of a single named test in a run.
type Status string

const (
	StatusPassed  Status = "PASSED"
	StatusFailed  Status = "FAILED"
	StatusError   Status = "ERROR"
	StatusSkipped Status = "SKIPPED"
	// StatusMissing means a required test was not found in the output at all —
	// distinct from FAILED, and usually a sign the patch broke collection or the
	// test name drifted.
	StatusMissing Status = "MISSING"
)

// LogParser extracts a per-test status map from raw test-runner output. Keeping
// it an interface-shaped function lets one eval flow serve pytest (real
// SWE-bench), go test (the offline reference fixtures), or any framework added
// later, without the scoring logic knowing which ran.
type LogParser func(output string) map[string]Status

// ParsePytest reads pytest output and returns a status per test node id. It
// handles the two common shapes:
//
//	tests/test_x.py::test_foo PASSED            [ 50%]   (pytest -v)
//	PASSED tests/test_x.py::test_foo                     (pytest -rA summary)
//
// The percentage column and any trailing noise are ignored; the node id is the
// token containing "::" (or ending in ".py").
func ParsePytest(output string) map[string]Status {
	out := map[string]Status{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		status, ok := pytestStatus(fields)
		if !ok {
			continue
		}
		node := pytestNode(fields)
		if node == "" {
			continue
		}
		// First definitive result for a node wins; pytest may echo a name in a
		// later summary block, but the inline result is authoritative.
		if _, seen := out[node]; !seen {
			out[node] = status
		}
	}
	return out
}

func pytestStatus(fields []string) (Status, bool) {
	for _, f := range fields {
		switch strings.TrimRight(f, ":") {
		case "PASSED":
			return StatusPassed, true
		case "FAILED":
			return StatusFailed, true
		case "ERROR":
			return StatusError, true
		case "SKIPPED":
			return StatusSkipped, true
		}
	}
	return "", false
}

func pytestNode(fields []string) string {
	for _, f := range fields {
		if strings.Contains(f, "::") || strings.HasSuffix(f, ".py") {
			return f
		}
	}
	return ""
}

// ParseGoTest reads `go test -v` output and returns a status per test name. It
// keys on the result lines:
//
//	--- PASS: TestFoo (0.00s)
//	--- FAIL: TestBar (0.00s)
//	--- SKIP: TestBaz (0.00s)
//
// Subtests (TestFoo/case_a) are reported under their full slash-path name.
func ParseGoTest(output string) map[string]Status {
	out := map[string]Status{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		var status Status
		switch {
		case strings.HasPrefix(line, "--- PASS:"):
			status = StatusPassed
		case strings.HasPrefix(line, "--- FAIL:"):
			status = StatusFailed
		case strings.HasPrefix(line, "--- SKIP:"):
			status = StatusSkipped
		default:
			continue
		}
		rest := strings.TrimSpace(line[strings.IndexByte(line, ':')+1:])
		name := strings.Fields(rest)
		if len(name) == 0 {
			continue
		}
		out[name[0]] = status
	}
	return out
}

// Score evaluates an instance against an observed per-test status map. An
// instance is resolved when every FAIL_TO_PASS test passes (the fix works) and
// every PASS_TO_PASS test still passes (no regression).
type Score struct {
	Resolved   bool
	FailToPass map[string]Status // required fixes → observed status
	PassToPass map[string]Status // required-still-passing → observed status
}

// ScoreInstance computes the Score for an instance from a status map.
func ScoreInstance(inst Instance, statuses map[string]Status) Score {
	s := Score{
		FailToPass: map[string]Status{},
		PassToPass: map[string]Status{},
	}
	allFixed := len(inst.FailToPass) > 0
	for _, name := range inst.FailToPass {
		st := statusFor(statuses, name)
		s.FailToPass[name] = st
		if st != StatusPassed {
			allFixed = false
		}
	}
	noRegression := true
	for _, name := range inst.PassToPass {
		st := statusFor(statuses, name)
		s.PassToPass[name] = st
		if st != StatusPassed {
			noRegression = false
		}
	}
	s.Resolved = allFixed && noRegression
	return s
}

func statusFor(statuses map[string]Status, name string) Status {
	if st, ok := statuses[name]; ok {
		return st
	}
	return StatusMissing
}
