// Package swebench is a SWE-bench-compatible evaluation harness: it loads real
// SWE-bench (Lite) instances, applies a candidate patch plus the held-out test
// patch to a checked-out repository, runs the specified tests, and scores the
// FAIL_TO_PASS / PASS_TO_PASS contract.
//
// The package is deliberately split into a pure core (schema, log parsing,
// scoring — all unit-testable offline with no network or Docker) and an
// Environment seam that abstracts where the repo lives and where commands run.
// Today a LocalEnv runs commands directly in a temp checkout; a Docker-backed
// Environment can slot in behind the same interface for real SWE-bench instances
// on CI runners, without touching the eval flow.
package swebench

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Instance is one SWE-bench task. The JSON tags match the upstream SWE-bench
// schema (e.g. the HuggingFace `princeton-nlp/SWE-bench_Lite` records), so real
// instances deserialize directly.
type Instance struct {
	InstanceID string `json:"instance_id"`
	Repo       string `json:"repo"`        // "owner/name", e.g. "psf/requests"
	BaseCommit string `json:"base_commit"` // SHA to check out before applying patches

	ProblemStatement string `json:"problem_statement"` // the issue text — the prompt for the agent

	// Patch is the gold (reference) solution; TestPatch adds/edits the tests that
	// encode the held-out contract. The agent is given the problem statement and
	// must produce its own patch; Patch/TestPatch are used by the baselines and to
	// score the result.
	Patch     string `json:"patch"`
	TestPatch string `json:"test_patch"`

	// Tests that must flip failing→passing (the bug fix) and tests that must stay
	// passing (no regression). Upstream sometimes encodes these as a JSON array
	// and sometimes as a JSON-encoded string; StringList accepts both.
	FailToPass StringList `json:"FAIL_TO_PASS"`
	PassToPass StringList `json:"PASS_TO_PASS"`

	EnvironmentSetupCommit string `json:"environment_setup_commit,omitempty"`
	Version                string `json:"version,omitempty"`
}

// Validate reports whether the instance carries the minimum needed to evaluate.
func (i *Instance) Validate() error {
	switch {
	case i == nil:
		return fmt.Errorf("instance is nil")
	case strings.TrimSpace(i.InstanceID) == "":
		return fmt.Errorf("instance_id is empty")
	case strings.TrimSpace(i.Repo) == "":
		return fmt.Errorf("%s: repo is empty", i.InstanceID)
	case strings.TrimSpace(i.BaseCommit) == "":
		return fmt.Errorf("%s: base_commit is empty", i.InstanceID)
	case strings.TrimSpace(i.TestPatch) == "":
		return fmt.Errorf("%s: test_patch is empty (no held-out tests to score against)", i.InstanceID)
	case len(i.FailToPass) == 0:
		return fmt.Errorf("%s: FAIL_TO_PASS is empty (nothing to resolve)", i.InstanceID)
	}
	return nil
}

// StringList is a []string that also unmarshals from a JSON-encoded string
// holding an array — i.e. both `["a","b"]` and `"[\"a\", \"b\"]"`. The SWE-bench
// datasets appear in both shapes depending on how they were exported, and a
// plain []string rejects the stringified form.
type StringList []string

func (l *StringList) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*l = nil
		return nil
	}
	// Stringified array: unwrap one layer of JSON string, then parse the inner.
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		s = strings.TrimSpace(s)
		if s == "" {
			*l = nil
			return nil
		}
		var inner []string
		if err := json.Unmarshal([]byte(s), &inner); err != nil {
			return fmt.Errorf("stringified FAIL/PASS list is not a JSON array: %w", err)
		}
		*l = inner
		return nil
	}
	var arr []string
	if err := json.Unmarshal(b, &arr); err != nil {
		return err
	}
	*l = arr
	return nil
}

// Load reads instances from a file that is either a JSON array of objects or
// JSON Lines (one object per line) — the two shapes SWE-bench exports take. It
// validates every instance so a malformed dataset fails loudly at load time
// rather than mid-run.
func Load(path string) ([]Instance, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := bufio.NewReader(f)
	// Peek the first non-space byte to decide array vs JSONL.
	first, err := firstNonSpace(r)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var insts []Instance
	if first == '[' {
		if err := json.NewDecoder(r).Decode(&insts); err != nil {
			return nil, fmt.Errorf("parse JSON array %s: %w", path, err)
		}
	} else {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024) // instances can be large
		line := 0
		for sc.Scan() {
			line++
			raw := bytes.TrimSpace(sc.Bytes())
			if len(raw) == 0 {
				continue
			}
			var inst Instance
			if err := json.Unmarshal(raw, &inst); err != nil {
				return nil, fmt.Errorf("%s line %d: %w", path, line, err)
			}
			insts = append(insts, inst)
		}
		if err := sc.Err(); err != nil {
			return nil, fmt.Errorf("scan %s: %w", path, err)
		}
	}

	for idx := range insts {
		if err := insts[idx].Validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	return insts, nil
}

// firstNonSpace returns the first non-whitespace byte without consuming it.
func firstNonSpace(r *bufio.Reader) (byte, error) {
	for {
		bs, err := r.Peek(1)
		if err != nil {
			return 0, err
		}
		switch bs[0] {
		case ' ', '\t', '\r', '\n':
			if _, err := r.Discard(1); err != nil {
				return 0, err
			}
		default:
			return bs[0], nil
		}
	}
}
