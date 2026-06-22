package orchestrator

import (
	"context"
	"log/slog"
	"time"

	"mugi/internal/agents"
	"mugi/internal/models"
	"mugi/internal/runner"
	"mugi/internal/state"
)

// Solo runs the single-call baseline: one coder agent turns the task into a
// complete artifact, which is then compiled and tested. There is no planner,
// reviewer, or revision loop. It exists so the bench can measure the full
// multi-agent pipeline against the honest control of a single well-prompted
// call — same provider, same task, same objective build/test scoring.
type Solo struct {
	coder    agents.Agent
	runTests bool
	log      *slog.Logger
}

// NewSolo returns a single-call runner backed by the given coder agent. When
// runTests is true the artifact is compiled and tested just like the pipeline,
// so the two strategies are scored on identical objective signals.
func NewSolo(coder agents.Agent, runTests bool, logger *slog.Logger) *Solo {
	if logger == nil {
		logger = slog.Default()
	}
	return &Solo{coder: coder, runTests: runTests, log: logger}
}

// Run executes the single-call pipeline for the task and returns the final
// state. The returned state has no plan and no reviews, so downstream readers
// see ReviewerScore=-1 and 0 revisions — which is exactly what "one call, no
// review loop" means.
func (s *Solo) Run(ctx context.Context, task *models.Task) (*state.WorkflowState, error) {
	st := state.New(task, 0)
	s.log.Info("solo run started", "task_id", task.ID)

	if err := s.coder.Process(ctx, st); err != nil {
		st.MarkFailed(err)
		return st, err
	}

	if s.runTests {
		res := runner.Run(ctx, st.GetArtifact(), 90*time.Second)
		st.SetExecResult(res)
		if res.Skipped {
			s.log.Info("executor skipped", "lang", res.Lang)
		} else {
			s.log.Info("executor done", "build_ok", res.BuildOK, "test_ok", res.TestOK)
		}
	}

	st.MarkCompleted()
	s.log.Info("solo run completed",
		"task_id", task.ID,
		"duration", time.Since(st.StartedAt).Round(time.Millisecond),
	)
	return st, nil
}
