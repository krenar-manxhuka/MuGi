package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const anthropicAPIURL = "https://api.anthropic.com/v1/messages"
const anthropicVersion = "2023-06-01"
const defaultAnthropicModel = "claude-sonnet-4-6"

// AnthropicProvider calls the Anthropic Messages API.
type AnthropicProvider struct {
	apiKey string
	model  string
	client *http.Client
}

// NewAnthropicProvider returns a provider that targets the Anthropic API.
// If model is empty, claude-sonnet-4-6 is used.
func NewAnthropicProvider(apiKey, model string) *AnthropicProvider {
	if model == "" {
		model = defaultAnthropicModel
	}
	return &AnthropicProvider{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *AnthropicProvider) Name() string { return "anthropic/" + p.model }

// --- wire types ---

type anthropicReq struct {
	Model     string        `json:"model"`
	MaxTokens int           `json:"max_tokens"`
	System    string        `json:"system,omitempty"`
	Messages  []anthropicMsg `json:"messages"`
}

type anthropicMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResp struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (p *AnthropicProvider) Generate(ctx context.Context, req Request) (Response, error) {
	msgs := make([]anthropicMsg, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = anthropicMsg{Role: m.Role, Content: m.Content}
	}

	maxTok := req.MaxTokens
	if maxTok == 0 {
		maxTok = 4096
	}

	body, err := json.Marshal(anthropicReq{
		Model:     p.model,
		MaxTokens: maxTok,
		System:    req.SystemPrompt,
		Messages:  msgs,
	})
	if err != nil {
		return Response{}, fmt.Errorf("anthropic: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicAPIURL, bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("anthropic: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("anthropic: http: %w", err)
	}
	defer resp.Body.Close()

	var result anthropicResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return Response{}, fmt.Errorf("anthropic: decode response: %w", err)
	}
	if result.Error != nil {
		return Response{}, fmt.Errorf("anthropic api error (%s): %s", result.Error.Type, result.Error.Message)
	}
	if len(result.Content) == 0 {
		return Response{}, fmt.Errorf("anthropic: empty content in response")
	}

	return Response{
		Content: result.Content[0].Text,
		Usage: Usage{
			InputTokens:  result.Usage.InputTokens,
			OutputTokens: result.Usage.OutputTokens,
		},
	}, nil
}
