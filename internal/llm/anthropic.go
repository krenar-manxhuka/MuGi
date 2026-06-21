package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

const anthropicAPIURL = "https://api.anthropic.com/v1/messages"
const anthropicVersion = "2023-06-01"
const defaultAnthropicModel = "claude-sonnet-4-6"

// Retry tuning. Transient failures (429 rate limits, 5xx, network blips) are
// retried with exponential backoff; this matters most on low-tier API keys
// whose per-minute limits are easy to brush against.
const (
	defaultMaxRetries  = 4
	defaultBackoffBase = 500 * time.Millisecond
	maxBackoff         = 30 * time.Second
)

// AnthropicProvider calls the Anthropic Messages API.
type AnthropicProvider struct {
	apiKey      string
	model       string
	client      *http.Client
	apiURL      string        // overridable in tests
	maxRetries  int           // number of retries after the first attempt
	backoffBase time.Duration // base delay for exponential backoff
}

// NewAnthropicProvider returns a provider that targets the Anthropic API.
// If model is empty, claude-sonnet-4-6 is used.
func NewAnthropicProvider(apiKey, model string) *AnthropicProvider {
	if model == "" {
		model = defaultAnthropicModel
	}
	return &AnthropicProvider{
		apiKey:      apiKey,
		model:       model,
		client:      &http.Client{Timeout: 120 * time.Second},
		apiURL:      anthropicAPIURL,
		maxRetries:  defaultMaxRetries,
		backoffBase: defaultBackoffBase,
	}
}

func (p *AnthropicProvider) Name() string { return "anthropic/" + p.model }

// --- wire types ---

type anthropicReq struct {
	Model     string         `json:"model"`
	MaxTokens int            `json:"max_tokens"`
	System    string         `json:"system,omitempty"`
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
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// retryableError marks a transient failure that is worth retrying. retryAfter,
// when non-zero, is the server-requested wait derived from the Retry-After header.
type retryableError struct {
	status     int
	message    string
	retryAfter time.Duration
}

func (e *retryableError) Error() string {
	if e.status == 0 {
		return fmt.Sprintf("anthropic: transient network error: %s", e.message)
	}
	return fmt.Sprintf("anthropic: transient api error (status %d): %s", e.status, e.message)
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

	var lastErr error
	for attempt := 0; ; attempt++ {
		resp, err := p.attempt(ctx, body)
		if err == nil {
			return resp, nil
		}
		lastErr = err

		// Only transient errors are retried; billing/auth/validation (4xx other
		// than 429) fail fast so we don't burn retries on a permanent problem.
		var re *retryableError
		if !errors.As(err, &re) || attempt >= p.maxRetries {
			return Response{}, lastErr
		}
		// Abort early if the caller's context is already done.
		if ctx.Err() != nil {
			return Response{}, ctx.Err()
		}

		wait := backoffDelay(attempt, p.backoffBase, re.retryAfter)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Response{}, ctx.Err()
		case <-timer.C:
		}
	}
}

// attempt performs a single HTTP request. A *retryableError is returned for
// failures worth retrying (network errors, 429, 5xx); all other failures are
// returned as terminal errors.
func (p *AnthropicProvider) attempt(ctx context.Context, body []byte) (Response, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.apiURL, bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("anthropic: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		// Don't retry if the caller cancelled or timed out — that's terminal.
		if ctx.Err() != nil {
			return Response{}, fmt.Errorf("anthropic: http: %w", err)
		}
		return Response{}, &retryableError{message: err.Error()}
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return Response{}, &retryableError{
			status:     resp.StatusCode,
			message:    "read body: " + err.Error(),
			retryAfter: parseRetryAfter(resp.Header),
		}
	}

	// Decode the body up front so we can surface the API's own error message
	// regardless of status code.
	var result anthropicResp
	decodeErr := json.Unmarshal(data, &result)

	// Transient statuses: retry. Use the structured message when present,
	// otherwise a truncated raw body.
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return Response{}, &retryableError{
			status:     resp.StatusCode,
			message:    errMessage(result, data),
			retryAfter: parseRetryAfter(resp.Header),
		}
	}

	// Terminal API error reported in the body (e.g. 400 billing, 401 auth).
	if result.Error != nil {
		return Response{}, fmt.Errorf("anthropic api error (%s): %s", result.Error.Type, result.Error.Message)
	}

	// Any other non-2xx without a structured error: surface status + raw body
	// rather than a misleading "empty content" further down.
	if resp.StatusCode != http.StatusOK {
		return Response{}, fmt.Errorf("anthropic: unexpected status %d: %s", resp.StatusCode, truncateBody(data))
	}

	if decodeErr != nil {
		return Response{}, fmt.Errorf("anthropic: decode response: %w", decodeErr)
	}
	if len(result.Content) == 0 {
		return Response{}, fmt.Errorf("anthropic: empty content in response")
	}

	return Response{
		Content:    result.Content[0].Text,
		StopReason: result.StopReason,
		Usage: Usage{
			InputTokens:  result.Usage.InputTokens,
			OutputTokens: result.Usage.OutputTokens,
		},
	}, nil
}

// errMessage prefers the API's structured error message, falling back to a
// truncated raw body when the response wasn't the expected JSON shape.
func errMessage(result anthropicResp, raw []byte) string {
	if result.Error != nil && result.Error.Message != "" {
		return result.Error.Message
	}
	return truncateBody(raw)
}

// truncateBody returns a single-line, length-bounded view of a response body
// suitable for embedding in an error message.
func truncateBody(b []byte) string {
	const max = 300
	s := string(bytes.TrimSpace(b))
	if s == "" {
		return "(empty body)"
	}
	if len(s) > max {
		s = s[:max] + "…"
	}
	return s
}

// parseRetryAfter reads the Retry-After header, supporting the delay-seconds
// form (e.g. "5"). The HTTP-date form is ignored in favour of exponential
// backoff. Returns 0 when absent or unparseable.
func parseRetryAfter(h http.Header) time.Duration {
	v := h.Get("Retry-After")
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return 0
}

// backoffDelay computes the wait before the next attempt: the larger of the
// server-requested Retry-After and an exponential backoff, capped at maxBackoff.
func backoffDelay(attempt int, base, retryAfter time.Duration) time.Duration {
	d := base << attempt // base * 2^attempt
	if d <= 0 || d > maxBackoff {
		d = maxBackoff
	}
	if retryAfter > d {
		d = retryAfter
	}
	if d > maxBackoff {
		d = maxBackoff
	}
	return d
}
