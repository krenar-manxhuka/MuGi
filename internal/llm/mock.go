package llm

import (
	"context"
	"fmt"
	"strings"
)

// MockProvider returns deterministic fake responses with no network or spend, so
// the whole pipeline can be exercised offline. A response is chosen by scanning
// the system prompt for the first matching CustomResponses key; with no match it
// echoes the last user message. Tests inject CustomResponses to script a reply
// (e.g. a canned unified diff).
type MockProvider struct {
	CustomResponses map[string]string
}

// NewMockProvider returns a MockProvider with no canned routes — it echoes, which
// is enough for a $0 dry run of the prediction pipeline.
func NewMockProvider() *MockProvider {
	return &MockProvider{}
}

func (m *MockProvider) Name() string { return "mock" }

func (m *MockProvider) Generate(_ context.Context, req Request) (Response, error) {
	prompt := strings.ToLower(req.SystemPrompt)
	for keyword, body := range m.CustomResponses {
		if strings.Contains(prompt, keyword) {
			return Response{
				Content: body,
				Usage:   Usage{InputTokens: 120, OutputTokens: 350},
			}, nil
		}
	}
	last := ""
	if len(req.Messages) > 0 {
		last = req.Messages[len(req.Messages)-1].Content
	}
	return Response{Content: fmt.Sprintf("(mock) no route matched; user said: %s", last)}, nil
}
