// Package config loads runtime configuration from environment variables.
package config

import (
	"os"
	"strconv"
)

// Config holds all tunable settings. Every field has a sensible default so the
// system runs out of the box with no configuration required.
type Config struct {
	// LLM_PROVIDER selects the backend: mock | anthropic | openai | ollama
	Provider string

	// LLM_MODEL overrides the default model for the chosen provider.
	Model string

	// MAX_REVISIONS caps the coder→reviewer loop.
	MaxRevisions int

	// OUTPUT_DIR is where artifact files are written after the workflow.
	OutputDir string

	// PROMPTS_DIR is the directory checked for prompt override templates.
	// Falls back to embedded templates when a file is not found here.
	PromptsDir string
}

// Load reads configuration from environment variables and applies defaults.
func Load() Config {
	return Config{
		Provider:     getEnv("LLM_PROVIDER", "mock"),
		Model:        getEnv("LLM_MODEL", ""),
		MaxRevisions: getEnvInt("MAX_REVISIONS", 3),
		OutputDir:    getEnv("OUTPUT_DIR", "output"),
		PromptsDir:   getEnv("PROMPTS_DIR", "prompts"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
