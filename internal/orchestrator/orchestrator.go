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
	"mugi/internal/runner"
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
	skipReview  bool
	runTests    bool
	log         *slog.Logger
}

// Config holds the knobs the caller can tune.
type Config struct {
	// MaxRevisions is the maximum number of code–review–revise cycles.
	// When this limit is reached the latest artifact is accepted regardless of
	// review outcome.
	MaxRevisions int

	// SkipReview accepts the coder's first output without running the reviewer.
	// Useful on slow or memory-constrained hardware.
	SkipReview bool

	// RunTests runs go build + go test on the coder's output before the reviewer sees it.
	RunTests bool

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
		skipReview:  cfg.SkipReview,
		runTests:    cfg.RunTests,
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
			if st.GetArtifact() != nil {
				// A previous revision exists — accept it rather than failing.
				o.log.Warn("coder failed on revision; accepting last successful artifact", "err", err)
				st.AddLog("orchestrator", "coder error on this revision — keeping prior artifact")
				break
			}
			st.MarkFailed(err)
			return st, err
		}

		if o.runTests {
			res := runner.Run(ctx, st.GetArtifact(), 90*time.Second)
			st.SetExecResult(res)
			if res.Skipped {
				o.log.Info("executor skipped", "lang", res.Lang)
			} else {
				o.log.Info("executor done", "build_ok", res.BuildOK, "test_ok", res.TestOK)
			}
		}

		if o.skipReview {
			o.log.Info("review skipped (SKIP_REVIEW=true)")
			st.AddLog("orchestrator", "review skipped — accepting coder output as-is")
			break
		}

		if err := o.run(ctx, o.reviewer, st, "review"); err != nil {
			// Reviewer produced malformed output — accept the current artifact.
			o.log.Warn("reviewer failed; accepting current artifact", "err", err)
			st.AddLog("orchestrator", "reviewer error — accepting current artifact as-is")
			break
		}

		review := st.LatestReview()
		approved := review != nil && review.Approved

		// ── Objective gate ────────────────────────────────────────────────────
		// A reviewer approval cannot ship an artifact whose build or tests are
		// red. The LLM reviewer has been observed (see bench failure analysis)
		// approving code at 9/10 that fails its own tests — even when handed the
		// failing output. When we have a real execution signal we trust it over
		// the reviewer's prose judgement and keep revising. This is a no-op when
		// RunTests is off (there is no objective signal to gate on).
		if approved && o.runTests {
			if exec := st.GetExecResult(); !objectiveOK(exec) {
				o.log.Warn("reviewer approved but build/test is red; overriding approval (objective gate)",
					"build_ok", exec.BuildOK, "test_ok", exec.TestOK, "score", review.Score)
				st.AddLog("orchestrator",
					"objective gate: reviewer approval overridden — build/test still failing, revising")
				st.RecordFalseApproval()
				approved = false
			}
		}

		if approved {
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

// objectiveOK reports whether the execution signal permits shipping an artifact.
// A nil or skipped result carries no signal and must not block (e.g. non-Go
// artifacts, or RunTests disabled). A real result ships only when both the build
// and the tests are green.
func objectiveOK(res *models.ExecResult) bool {
	if res == nil || res.Skipped {
		return true
	}
	return res.BuildOK && res.TestOK
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
