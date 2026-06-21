// Package models defines the shared data contracts used across all agents and the orchestrator.
package models

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"mugi/internal/fsafe"
)

// WorkflowStatus represents the current phase of the pipeline.
type WorkflowStatus string

const (
	StatusPending   WorkflowStatus = "pending"
	StatusPlanning  WorkflowStatus = "planning"
	StatusCoding    WorkflowStatus = "coding"
	StatusReviewing WorkflowStatus = "reviewing"
	StatusRevising  WorkflowStatus = "revising"
	StatusCompleted WorkflowStatus = "completed"
	StatusFailed    WorkflowStatus = "failed"
)

// Task is the original user request that drives the entire workflow.
type Task struct {
	ID          string    `json:"id"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

// Plan is the structured execution plan produced by the Planner agent.
type Plan struct {
	Summary      string   `json:"summary"`
	Steps        []Step   `json:"steps"`
	Dependencies []string `json:"dependencies"`
	Milestones   []string `json:"milestones"`
	Risks        []string `json:"risks"`
	Assumptions  []string `json:"assumptions"`
}

// Step is a single action item within a Plan.
type Step struct {
	ID          int     `json:"id"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	DependsOn   IntList `json:"depends_on,omitempty"`
}

// IntList is a []int that also unmarshals from numeric strings. Some models
// emit step dependencies as JSON strings (e.g. ["1", "2"]) rather than numbers,
// which a plain []int rejects. This keeps the plan parseable without weakening
// the contract for compliant models, which still emit bare ints.
type IntList []int

func (l *IntList) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*l = nil
		return nil
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	out := make([]int, 0, len(raw))
	for _, elem := range raw {
		elem = bytes.TrimSpace(elem)
		if len(elem) >= 2 && elem[0] == '"' && elem[len(elem)-1] == '"' {
			var s string
			if err := json.Unmarshal(elem, &s); err != nil {
				return err
			}
			n, err := strconv.Atoi(s)
			if err != nil {
				return fmt.Errorf("depends_on: %q is not an integer: %w", s, err)
			}
			out = append(out, n)
			continue
		}
		var n int
		if err := json.Unmarshal(elem, &n); err != nil {
			return err
		}
		out = append(out, n)
	}
	*l = out
	return nil
}

// Artifact is the code output produced by the Coder agent.
type Artifact struct {
	Summary  string `json:"summary"`
	Revision int    `json:"revision"`
	Files    []File `json:"files"`
}

// File is a single source or config file inside an Artifact.
type File struct {
	Path    string `json:"path"`
	Lang    string `json:"lang"`
	Content string `json:"content"`
}

// Review is the structured feedback produced by the Reviewer agent.
type Review struct {
	Approved bool    `json:"approved"`
	Score    int     `json:"score"` // 0–10
	Feedback string  `json:"feedback"`
	Issues   []Issue `json:"issues,omitempty"`
	Revision int     `json:"revision"`
}

// Severity classifies a review Issue. Using a named type instead of a bare
// string documents the allowed set and keeps the vocabulary in one place.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityMajor    Severity = "major"
	SeverityMinor    Severity = "minor"
)

// Issue is a specific problem found during review.
type Issue struct {
	Severity    Severity `json:"severity"` // critical | major | minor
	File        string   `json:"file,omitempty"`
	Description string   `json:"description"`
	Suggestion  string   `json:"suggestion"`
}

// LogEntry records a timestamped message from a named agent.
type LogEntry struct {
	Agent   string    `json:"agent"`
	Message string    `json:"message"`
	At      time.Time `json:"at"`
}

// ExecResult holds the output from compiling, testing, and vetting a generated artifact.
type ExecResult struct {
	Lang     string // language detected from artifact files
	BuildOK  bool
	BuildOut string // combined stdout+stderr of build command
	TestOK   bool
	TestOut  string // combined stdout+stderr of test command
	VetOK    bool   // go vet exit status (only meaningful when BuildOK)
	VetOut   string // combined stdout+stderr of vet command
	Skipped  bool   // true when the language has no executor
}

// --- validation (the semantic layer of the LLM→domain boundary) -----------
//
// These methods reject parsed-but-meaningless output before it flows downstream,
// turning a vague later failure into a precise, early one. They check invariants
// that genuinely break consumers (empty plans, unsafe paths, out-of-range scores)
// and deliberately stay lenient on cosmetic fields to avoid spurious failures.

// Validate reports whether the task is well-formed enough to drive the pipeline.
func (t *Task) Validate() error {
	if t == nil {
		return fmt.Errorf("task is nil")
	}
	if strings.TrimSpace(t.Description) == "" {
		return fmt.Errorf("task description is empty")
	}
	return nil
}

// Validate reports whether the plan is usable by the Coder.
func (p *Plan) Validate() error {
	if p == nil {
		return fmt.Errorf("plan is nil")
	}
	if len(p.Steps) == 0 {
		return fmt.Errorf("plan has no steps")
	}
	for _, s := range p.Steps {
		if strings.TrimSpace(s.Title) == "" {
			return fmt.Errorf("step %d has an empty title", s.ID)
		}
	}
	return nil
}

// Validate reports whether the artifact is safe and complete enough to write
// and build. Every file path must be a contained relative path (no traversal).
func (a *Artifact) Validate() error {
	if a == nil {
		return fmt.Errorf("artifact is nil")
	}
	if len(a.Files) == 0 {
		return fmt.Errorf("artifact has no files")
	}
	for i, f := range a.Files {
		if err := fsafe.SafeRelPath(f.Path); err != nil {
			return fmt.Errorf("file %d: %w", i, err)
		}
		if strings.TrimSpace(f.Content) == "" {
			return fmt.Errorf("file %q has empty content", f.Path)
		}
	}
	return nil
}

// Validate reports whether the review is internally consistent. The score drives
// orchestration decisions, so it must be within range.
func (r *Review) Validate() error {
	if r == nil {
		return fmt.Errorf("review is nil")
	}
	if r.Score < 0 || r.Score > 10 {
		return fmt.Errorf("score %d out of range [0,10]", r.Score)
	}
	return nil
}
