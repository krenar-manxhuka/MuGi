// Package state holds the mutable workflow context shared across all agents.
package state

import (
	"sync"
	"time"

	"mugi/internal/models"
)

// WorkflowState is the single source of truth for a running pipeline.
// All agent writes go through the provided methods so locking is centralised.
type WorkflowState struct {
	mu sync.RWMutex

	Task      *models.Task
	Plan      *models.Plan
	Artifact  *models.Artifact
	Reviews   []*models.Review
	Status    models.WorkflowStatus
	Iteration int
	MaxIter   int
	Log       []models.LogEntry

	StartedAt   time.Time
	CompletedAt *time.Time
	Error       error
}

// New returns an initialised WorkflowState for the given task.
func New(task *models.Task, maxIter int) *WorkflowState {
	return &WorkflowState{
		Task:      task,
		Status:    models.StatusPending,
		MaxIter:   maxIter,
		StartedAt: time.Now(),
	}
}

// --- status ---

func (s *WorkflowState) SetStatus(st models.WorkflowStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Status = st
}

func (s *WorkflowState) GetStatus() models.WorkflowStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Status
}

// --- plan ---

func (s *WorkflowState) SetPlan(p *models.Plan) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Plan = p
}

func (s *WorkflowState) GetPlan() *models.Plan {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Plan
}

// --- artifact ---

func (s *WorkflowState) SetArtifact(a *models.Artifact) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Artifact = a
}

func (s *WorkflowState) GetArtifact() *models.Artifact {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Artifact
}

// --- reviews ---

func (s *WorkflowState) AddReview(r *models.Review) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Reviews = append(s.Reviews, r)
}

func (s *WorkflowState) LatestReview() *models.Review {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.Reviews) == 0 {
		return nil
	}
	return s.Reviews[len(s.Reviews)-1]
}

func (s *WorkflowState) AllReviews() []*models.Review {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*models.Review, len(s.Reviews))
	copy(out, s.Reviews)
	return out
}

// --- iteration ---

func (s *WorkflowState) IncrementIteration() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Iteration++
}

func (s *WorkflowState) MaxIterReached() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Iteration >= s.MaxIter
}

// --- log ---

func (s *WorkflowState) AddLog(agent, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Log = append(s.Log, models.LogEntry{
		Agent:   agent,
		Message: message,
		At:      time.Now(),
	})
}

func (s *WorkflowState) GetLog() []models.LogEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]models.LogEntry, len(s.Log))
	copy(out, s.Log)
	return out
}

// --- completion ---

func (s *WorkflowState) MarkCompleted() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.CompletedAt = &now
	s.Status = models.StatusCompleted
}

func (s *WorkflowState) MarkFailed(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.CompletedAt = &now
	s.Status = models.StatusFailed
	s.Error = err
}

// Snapshot returns an immutable copy of iteration and status for safe reading.
func (s *WorkflowState) Snapshot() (iter int, maxIter int, status models.WorkflowStatus) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Iteration, s.MaxIter, s.Status
}
