package llm

import (
	"context"
	"strings"
	"testing"
)

// countingStub is a trivial Provider that records how many times it was called.
type countingStub struct{ calls int }

func (c *countingStub) Name() string { return "stub" }
func (c *countingStub) Generate(_ context.Context, _ Request) (Response, error) {
	c.calls++
	return Response{Content: "ok"}, nil
}

func TestBudgetProviderEnforcesLimit(t *testing.T) {
	inner := &countingStub{}
	b := NewBudgetProvider(inner, 3)

	for i := 0; i < 3; i++ {
		if _, err := b.Generate(context.Background(), Request{}); err != nil {
			t.Fatalf("call %d should succeed, got: %v", i+1, err)
		}
	}
	// 4th call must be rejected without reaching the inner provider.
	_, err := b.Generate(context.Background(), Request{})
	if err == nil {
		t.Fatal("expected the 4th call to exceed the budget")
	}
	if !strings.Contains(err.Error(), "budget exceeded") {
		t.Fatalf("unexpected error: %v", err)
	}
	if inner.calls != 3 {
		t.Fatalf("inner provider should have been called exactly 3 times, got %d", inner.calls)
	}
	if b.Calls() != 3 {
		t.Fatalf("budget should report 3 successful calls, got %d", b.Calls())
	}
}

func TestBudgetProviderUnlimitedWhenZero(t *testing.T) {
	inner := &countingStub{}
	b := NewBudgetProvider(inner, 0) // 0 disables the cap

	for i := 0; i < 10; i++ {
		if _, err := b.Generate(context.Background(), Request{}); err != nil {
			t.Fatalf("unlimited budget should not error, got: %v", err)
		}
	}
	if inner.calls != 10 {
		t.Fatalf("expected 10 calls, got %d", inner.calls)
	}
}
