// Package llm defines the plug-and-play LLM provider abstraction.
// Agent code only imports this package; swapping vendors requires zero agent changes.
package llm

import "context"

// maxResponseBytes caps how much of an HTTP response body a provider will read,
// so a malformed or runaway upstream response can't exhaust memory. Generous
// relative to real payloads (a 16K-token completion is well under 1 MB).
const maxResponseBytes = 16 << 20 // 16 MiB

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

	// StopReason is the provider's reason for ending generation, normalised to
	// the Anthropic vocabulary ("end_turn", "max_tokens", ...). It is empty for
	// providers that don't report it (e.g. the mock). A value of "max_tokens"
	// means the output was truncated against the request's MaxTokens ceiling.
	StopReason string
}

// Truncated reports whether generation stopped because it hit the MaxTokens
// ceiling, which typically leaves structured output (e.g. JSON) incomplete.
func (r Response) Truncated() bool { return r.StopReason == "max_tokens" }

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
