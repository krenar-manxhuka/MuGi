// Package orchestrator implements the workflow engine that sequences agents,
// manages state transitions, and enforces bounded review loops.
package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"mugi/internal/agents"
	"mugi/internal/models"
	"mugi/internal/state"
)

// Orchestrator drives the multi-agent pipeline from start to finish.
// It is intentionally decoupled from any specific agent implementation or LLM
// vendor; dependencies are injected via the constructor.
type Orchestrator struct {
	coordinator agents.Agent
	planner     agents.Agent
	coder       agents.Agent
	reviewer    agents.Agent
	maxIter     int
	log         *slog.Logger
}

// Config holds the knobs the caller can tune.
type Config struct {
	// MaxRevisions is the maximum number of code–review–revise cycles.
	// When this limit is reached the latest artifact is accepted regardless of
	// review outcome.
	MaxRevisions int

	// Logger is optional; a default text logger is used when nil.
	Logger *slog.Logger
}

// New returns an Orchestrator with the provided agents and config.
func New(
	coordinator, planner, coder, reviewer agents.Agent,
	cfg Config,
) *Orchestrator {
	if cfg.MaxRevisions <= 0 {
		cfg.MaxRevisions = 3
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Orchestrator{
		coordinator: coordinator,
		planner:     planner,
		coder:       coder,
		reviewer:    reviewer,
		maxIter:     cfg.MaxRevisions,
		log:         logger,
	}
}

// Run executes the full pipeline for the given task and returns the final state.
//
// Workflow:
//
//	coordinator (init) → planner → [coder → reviewer]* → coordinator (summary)
//
// The inner loop repeats until the reviewer approves the artifact or the
// maximum revision count is reached.
func (o *Orchestrator) Run(ctx context.Context, task *models.Task) (*state.WorkflowState, error) {
	st := state.New(task, o.maxIter)

	o.log.Info("workflow started", "task_id", task.ID, "max_revisions", o.maxIter)

	// ── Phase 1: Coordinator initialises the workflow ─────────────────────────
	if err := o.run(ctx, o.coordinator, st, "init"); err != nil {
		st.MarkFailed(err)
		return st, err
	}

	// ── Phase 2: Planner builds the execution plan ────────────────────────────
	if err := o.run(ctx, o.planner, st, "plan"); err != nil {
		st.MarkFailed(err)
		return st, err
	}

	// ── Phase 3: Coder + Reviewer loop ────────────────────────────────────────
	for {
		if err := o.run(ctx, o.coder, st, "code"); err != nil {
			st.MarkFailed(err)
			return st, err
		}

		if err := o.run(ctx, o.reviewer, st, "review"); err != nil {
			st.MarkFailed(err)
			return st, err
		}

		review := st.LatestReview()
		if review != nil && review.Approved {
			o.log.Info("artifact approved", "score", review.Score, "revision", review.Revision)
			break
		}

		st.IncrementIteration()

		if st.MaxIterReached() {
			o.log.Warn("max revisions reached; accepting latest artifact",
				"iteration", o.maxIter)
			st.AddLog("orchestrator",
				fmt.Sprintf("max revisions (%d) reached — accepting latest output", o.maxIter))
			break
		}

		// Coordinator narrates the revision decision
		st.SetStatus(models.StatusRevising)
		if err := o.run(ctx, o.coordinator, st, "revise"); err != nil {
			// Non-fatal: log and continue without coordinator commentary
			o.log.Warn("coordinator revise step failed; continuing", "err", err)
		}
	}

	// ── Phase 4: Coordinator produces the final summary ───────────────────────
	st.SetStatus(models.StatusCompleted)
	if err := o.run(ctx, o.coordinator, st, "summary"); err != nil {
		o.log.Warn("coordinator summary step failed", "err", err)
	}

	st.MarkCompleted()

	iter, _, _ := st.Snapshot()
	o.log.Info("workflow completed",
		"task_id", task.ID,
		"revisions", iter,
		"duration", time.Since(st.StartedAt).Round(time.Millisecond),
	)
	return st, nil
}

// run calls an agent and emits structured log lines around the call.
func (o *Orchestrator) run(ctx context.Context, a agents.Agent, st *state.WorkflowState, phase string) error {
	o.log.Info("agent starting", "agent", a.Role(), "phase", phase)
	start := time.Now()

	if err := a.Process(ctx, st); err != nil {
		o.log.Error("agent failed",
			"agent", a.Role(),
			"phase", phase,
			"duration", time.Since(start).Round(time.Millisecond),
			"err", err,
		)
		return fmt.Errorf("[%s/%s] %w", a.Role(), phase, err)
	}

	o.log.Info("agent done",
		"agent", a.Role(),
		"phase", phase,
		"duration", time.Since(start).Round(time.Millisecond),
	)
	return nil
}
