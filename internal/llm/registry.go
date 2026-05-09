package llm

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// NewFromEnv constructs a Provider from environment variables.
//
// Variables:
//
//	LLM_PROVIDER    mock | anthropic | openai | ollama   (default: mock)
//	LLM_MODEL       model name override (optional; each provider has a sensible default)
//	LLM_TIMEOUT     HTTP timeout in seconds (default: 120 for openai, 300 for ollama)
//
// Provider-specific:
//
//	ANTHROPIC_API_KEY   required when LLM_PROVIDER=anthropic
//	OPENAI_API_KEY      required when LLM_PROVIDER=openai
//	OPENAI_BASE_URL     override base URL (e.g. for proxies)
//	OLLAMA_BASE_URL     defaults to http://localhost:11434/v1
//	OLLAMA_NUM_CTX      context window size in tokens (default: 8192)
func NewFromEnv() (Provider, error) {
	p := strings.ToLower(strings.TrimSpace(os.Getenv("LLM_PROVIDER")))
	if p == "" {
		p = "mock"
	}

	model := os.Getenv("LLM_MODEL")

	switch p {
	case "mock":
		return NewMockProvider(), nil

	case "anthropic":
		key := os.Getenv("ANTHROPIC_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("LLM_PROVIDER=anthropic requires ANTHROPIC_API_KEY to be set")
		}
		return NewAnthropicProvider(key, model), nil

	case "openai":
		key := os.Getenv("OPENAI_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("LLM_PROVIDER=openai requires OPENAI_API_KEY to be set")
		}
		baseURL := os.Getenv("OPENAI_BASE_URL")
		if model == "" {
			model = "gpt-4o"
		}
		return NewOpenAIProvider(baseURL, key, model, parseTimeout(120), 0), nil

	case "ollama":
		baseURL := os.Getenv("OLLAMA_BASE_URL")
		if baseURL == "" {
			baseURL = "http://localhost:11434/v1"
		}
		if model == "" {
			model = "llama3"
		}
		numCtx := parseEnvInt("OLLAMA_NUM_CTX", 8192)
		// Ollama's OpenAI-compatible endpoint needs no API key.
		// Default to 5 minutes — local models on CPU/GPU split are slow.
		return NewOpenAIProvider(baseURL, "", model, parseTimeout(300), numCtx), nil

	default:
		return nil, fmt.Errorf("unknown LLM_PROVIDER %q; valid values: mock, anthropic, openai, ollama", p)
	}
}

// parseTimeout reads LLM_TIMEOUT (seconds) from the environment, falling back to defaultSec.
func parseTimeout(defaultSec int) time.Duration {
	if v := os.Getenv("LLM_TIMEOUT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return time.Duration(defaultSec) * time.Second
}

func parseEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}
