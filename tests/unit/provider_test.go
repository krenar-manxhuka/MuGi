package unit_test

import (
	"context"
	"testing"

	"mugi/internal/llm"
)

// TestMockProviderImplementsInterface verifies that MockProvider satisfies the
// Provider interface at compile time.
func TestMockProviderImplementsInterface(t *testing.T) {
	var _ llm.Provider = llm.NewMockProvider()
}

func TestMockProviderName(t *testing.T) {
	p := llm.NewMockProvider()
	if p.Name() != "mock" {
		t.Fatalf("expected name 'mock', got %q", p.Name())
	}
}

func TestMockProviderRoutesPlanner(t *testing.T) {
	p := llm.NewMockProvider()
	resp, err := p.Generate(context.Background(), llm.Request{
		SystemPrompt: "You are the planner agent.",
		Messages:     []llm.Message{{Role: "user", Content: "plan this"}},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if resp.Content == "" {
		t.Fatal("expected non-empty content")
	}
}

func TestMockProviderRoutesCoder(t *testing.T) {
	p := llm.NewMockProvider()
	resp, err := p.Generate(context.Background(), llm.Request{
		SystemPrompt: "You are the coder agent.",
		Messages:     []llm.Message{{Role: "user", Content: "implement this"}},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if resp.Content == "" {
		t.Fatal("expected non-empty content")
	}
}

func TestMockProviderRoutesReviewer(t *testing.T) {
	p := llm.NewMockProvider()
	resp, err := p.Generate(context.Background(), llm.Request{
		SystemPrompt: "You are the reviewer agent.",
		Messages:     []llm.Message{{Role: "user", Content: "review this"}},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if resp.Content == "" {
		t.Fatal("expected non-empty content")
	}
}

func TestMockProviderFallback(t *testing.T) {
	p := llm.NewMockProvider()
	resp, err := p.Generate(context.Background(), llm.Request{
		SystemPrompt: "Some unknown system prompt with no keyword match.",
		Messages:     []llm.Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("expected no error on fallback, got: %v", err)
	}
	if resp.Content == "" {
		t.Fatal("expected fallback content")
	}
}

func TestAnthropicProviderImplementsInterface(t *testing.T) {
	// Compile-time check only — no API call is made.
	var _ llm.Provider = llm.NewAnthropicProvider("key", "model")
}

func TestOpenAIProviderImplementsInterface(t *testing.T) {
	var _ llm.Provider = llm.NewOpenAIProvider("", "key", "model", 0, 0)
}

func TestAnthropicProviderName(t *testing.T) {
	p := llm.NewAnthropicProvider("key", "my-model")
	if p.Name() != "anthropic/my-model" {
		t.Fatalf("unexpected name: %s", p.Name())
	}
}

func TestOpenAIProviderName(t *testing.T) {
	p := llm.NewOpenAIProvider("", "key", "gpt-4o", 0, 0)
	if p.Name() != "openai/gpt-4o" {
		t.Fatalf("unexpected name: %s", p.Name())
	}
}

func TestCustomMockResponse(t *testing.T) {
	p := &llm.MockProvider{
		CustomResponses: map[string]string{
			"custom-role": `{"hello":"world"}`,
		},
	}
	resp, err := p.Generate(context.Background(), llm.Request{
		SystemPrompt: "You are a custom-role agent.",
		Messages:     []llm.Message{{Role: "user", Content: "go"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != `{"hello":"world"}` {
		t.Fatalf("unexpected content: %s", resp.Content)
	}
}
