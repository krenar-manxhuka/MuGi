package llm

import (
	"context"
	"fmt"
	"sync"
)

// BudgetProvider wraps a Provider and enforces a hard ceiling on the number of
// Generate calls per run. It is a defence-in-depth guardrail: the orchestrator
// already bounds the revision loop, but a misconfiguration (e.g. a very high
// MAX_REVISIONS) or a future bug should never be able to rack up unbounded API
// spend. Once the limit is reached, further calls fail fast instead of spending.
//
// The count is logical Generate calls — a provider's internal HTTP retries count
// as one — so the ceiling maps directly to billable model invocations.
type BudgetProvider struct {
	inner    Provider
	maxCalls int

	mu    sync.Mutex
	calls int
}

// NewBudgetProvider caps inner at maxCalls Generate calls. A maxCalls <= 0
// disables the cap (unlimited).
func NewBudgetProvider(inner Provider, maxCalls int) *BudgetProvider {
	return &BudgetProvider{inner: inner, maxCalls: maxCalls}
}

func (b *BudgetProvider) Name() string { return b.inner.Name() }

// Calls returns how many Generate calls have been made so far.
func (b *BudgetProvider) Calls() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

func (b *BudgetProvider) Generate(ctx context.Context, req Request) (Response, error) {
	b.mu.Lock()
	if b.maxCalls > 0 && b.calls >= b.maxCalls {
		b.mu.Unlock()
		return Response{}, fmt.Errorf("llm budget exceeded: reached the %d-call limit for this run (raise MAX_LLM_CALLS to allow more)", b.maxCalls)
	}
	b.calls++
	b.mu.Unlock()
	return b.inner.Generate(ctx, req)
}
