// Package models defines the shared data contracts used across all agents and the orchestrator.
package models

import "time"

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
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	DependsOn   []int  `json:"depends_on,omitempty"`
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

// Issue is a specific problem found during review.
type Issue struct {
	Severity    string `json:"severity"` // "critical" | "major" | "minor"
	File        string `json:"file,omitempty"`
	Description string `json:"description"`
	Suggestion  string `json:"suggestion"`
}

// LogEntry records a timestamped message from a named agent.
type LogEntry struct {
	Agent   string    `json:"agent"`
	Message string    `json:"message"`
	At      time.Time `json:"at"`
}

// ExecResult holds the output from compiling and testing a generated artifact.
type ExecResult struct {
	Lang     string // language detected from artifact files
	BuildOK  bool
	BuildOut string // combined stdout+stderr of build command
	TestOK   bool
	TestOut  string // combined stdout+stderr of test command
	Skipped  bool   // true when the language has no executor
}
