// Package llm defines the plug-and-play LLM provider abstraction.
// Agent code only imports this package; swapping vendors requires zero agent changes.
package llm

import "context"

// Message is a single turn in a conversation.
type Message struct {
	Role    string // "user" | "assistant"
	Content string
}

// Request is the vendor-neutral input to a provider.
type Request struct {
	SystemPrompt string
	Messages     []Message
	MaxTokens    int
	Temperature  float64
}

// Response is the vendor-neutral output from a provider.
type Response struct {
	Content string
	Usage   Usage
}

// Usage tracks token consumption; may be zero-valued for providers that don't report it.
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// Provider is the single interface every LLM backend must satisfy.
// Implement this interface to add any model, vendor, or deployment target.
type Provider interface {
	// Generate sends a request and returns the model's reply.
	Generate(ctx context.Context, req Request) (Response, error)

	// Name returns a human-readable identifier such as "anthropic/claude-sonnet-4-6".
	Name() string
}
