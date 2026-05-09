package llm

import (
	"fmt"
	"os"
	"strings"
)

// NewFromEnv constructs a Provider from environment variables.
//
// Variables:
//
//	LLM_PROVIDER   mock | anthropic | openai | ollama   (default: mock)
//	LLM_MODEL      model name override (optional; each provider has a sensible default)
//
// Provider-specific:
//
//	ANTHROPIC_API_KEY   required when LLM_PROVIDER=anthropic
//	OPENAI_API_KEY      required when LLM_PROVIDER=openai
//	OPENAI_BASE_URL     override base URL (e.g. for proxies)
//	OLLAMA_BASE_URL     defaults to http://localhost:11434/v1
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
		return NewOpenAIProvider(baseURL, key, model), nil

	case "ollama":
		baseURL := os.Getenv("OLLAMA_BASE_URL")
		if baseURL == "" {
			baseURL = "http://localhost:11434/v1"
		}
		if model == "" {
			model = "llama3"
		}
		// Ollama's OpenAI-compatible endpoint needs no API key
		return NewOpenAIProvider(baseURL, "", model), nil

	default:
		return nil, fmt.Errorf("unknown LLM_PROVIDER %q; valid values: mock, anthropic, openai, ollama", p)
	}
}
