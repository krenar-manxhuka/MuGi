package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// OpenAIProvider calls any OpenAI-compatible chat completions endpoint.
//
// This covers:
//   - OpenAI (https://api.openai.com/v1)
//   - Ollama  (http://localhost:11434/v1)
//   - LM Studio, LocalAI, vLLM, Groq, Together AI, Fireworks, Mistral, …
//
// Pass an empty apiKey to skip the Authorization header (for local endpoints).
type OpenAIProvider struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

// NewOpenAIProvider returns an OpenAI-compatible provider.
// baseURL defaults to https://api.openai.com/v1 when empty.
func NewOpenAIProvider(baseURL, apiKey, model string) *OpenAIProvider {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	return &OpenAIProvider{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		client:  &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *OpenAIProvider) Name() string { return "openai/" + p.model }

// --- wire types ---

type openAIReq struct {
	Model       string       `json:"model"`
	Messages    []openAIMsg  `json:"messages"`
	MaxTokens   int          `json:"max_tokens,omitempty"`
	Temperature float64      `json:"temperature,omitempty"`
}

type openAIMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResp struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error,omitempty"`
}

func (p *OpenAIProvider) Generate(ctx context.Context, req Request) (Response, error) {
	msgs := make([]openAIMsg, 0, len(req.Messages)+1)
	// OpenAI puts the system prompt as a system-role message
	if req.SystemPrompt != "" {
		msgs = append(msgs, openAIMsg{Role: "system", Content: req.SystemPrompt})
	}
	for _, m := range req.Messages {
		msgs = append(msgs, openAIMsg{Role: m.Role, Content: m.Content})
	}

	maxTok := req.MaxTokens
	if maxTok == 0 {
		maxTok = 4096
	}

	body, err := json.Marshal(openAIReq{
		Model:       p.model,
		Messages:    msgs,
		MaxTokens:   maxTok,
		Temperature: req.Temperature,
	})
	if err != nil {
		return Response{}, fmt.Errorf("openai: marshal request: %w", err)
	}

	url := p.baseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("openai: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("openai: http: %w", err)
	}
	defer resp.Body.Close()

	var result openAIResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return Response{}, fmt.Errorf("openai: decode response: %w", err)
	}
	if result.Error != nil {
		return Response{}, fmt.Errorf("openai api error (%s): %s", result.Error.Code, result.Error.Message)
	}
	if len(result.Choices) == 0 {
		return Response{}, fmt.Errorf("openai: no choices in response")
	}

	return Response{
		Content: result.Choices[0].Message.Content,
		Usage: Usage{
			InputTokens:  result.Usage.PromptTokens,
			OutputTokens: result.Usage.CompletionTokens,
		},
	}, nil
}
