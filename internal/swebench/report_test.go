package swebench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParsePytest(t *testing.T) {
	out := `
============================= test session starts =============================
tests/test_core.py::test_fix PASSED                                      [ 33%]
tests/test_core.py::test_regress FAILED                                  [ 66%]
tests/test_core.py::test_skip SKIPPED                                    [100%]
PASSED tests/test_summary.py::test_extra
`
	got := ParsePytest(out)
	want := map[string]Status{
		"tests/test_core.py::test_fix":      StatusPassed,
		"tests/test_core.py::test_regress":  StatusFailed,
		"tests/test_core.py::test_skip":     StatusSkipped,
		"tests/test_summary.py::test_extra": StatusPassed,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("pytest %s = %q, want %q", k, got[k], v)
		}
	}
}

func TestParseGoTest(t *testing.T) {
	out := `=== RUN   TestFix
--- PASS: TestFix (0.00s)
=== RUN   TestRegress
--- FAIL: TestRegress (0.01s)
=== RUN   TestSkipped
--- SKIP: TestSkipped (0.00s)
PASS
`
	got := ParseGoTest(out)
	if got["TestFix"] != StatusPassed || got["TestRegress"] != StatusFailed || got["TestSkipped"] != StatusSkipped {
		t.Fatalf("gotest parse wrong: %+v", got)
	}
}

func TestScoreInstance(t *testing.T) {
	inst := Instance{
		FailToPass: StringList{"TestFix"},
		PassToPass: StringList{"TestKeep"},
	}
	cases := []struct {
		name     string
		statuses map[string]Status
		resolved bool
	}{
		{"all green", map[string]Status{"TestFix": StatusPassed, "TestKeep": StatusPassed}, true},
		{"fix still broken", map[string]Status{"TestFix": StatusFailed, "TestKeep": StatusPassed}, false},
		{"regression", map[string]Status{"TestFix": StatusPassed, "TestKeep": StatusFailed}, false},
		{"fix missing", map[string]Status{"TestKeep": StatusPassed}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ScoreInstance(inst, c.statuses).Resolved; got != c.resolved {
				t.Fatalf("Resolved = %v, want %v", got, c.resolved)
			}
		})
	}
}

func TestStringListUnmarshal(t *testing.T) {
	// Plain array and stringified array must both decode to the same slice.
	for _, raw := range []string{
		`{"FAIL_TO_PASS": ["a::x", "b::y"]}`,
		`{"FAIL_TO_PASS": "[\"a::x\", \"b::y\"]"}`,
	} {
		var inst Instance
		if err := json.Unmarshal([]byte(raw), &inst); err != nil {
			t.Fatalf("unmarshal %s: %v", raw, err)
		}
		if len(inst.FailToPass) != 2 || inst.FailToPass[0] != "a::x" || inst.FailToPass[1] != "b::y" {
			t.Fatalf("got %v from %s", inst.FailToPass, raw)
		}
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	jsonl := filepath.Join(dir, "lite.jsonl")
	content := `{"instance_id":"x__1","repo":"x/y","base_commit":"abc","problem_statement":"fix it","test_patch":"diff","FAIL_TO_PASS":["t::a"],"PASS_TO_PASS":["t::b"]}
{"instance_id":"x__2","repo":"x/y","base_commit":"def","problem_statement":"fix more","test_patch":"diff","FAIL_TO_PASS":"[\"t::c\"]","PASS_TO_PASS":[]}
`
	if err := os.WriteFile(jsonl, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	insts, err := Load(jsonl)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(insts) != 2 {
		t.Fatalf("got %d instances, want 2", len(insts))
	}
	if insts[1].FailToPass[0] != "t::c" {
		t.Fatalf("stringified list not decoded: %v", insts[1].FailToPass)
	}
}
