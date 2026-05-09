TASK ?= "Build a minimal Go HTTP server with a /health endpoint and graceful shutdown"
GO   := go

# Only the binary extension differs between platforms.
# make on Windows typically runs via Git Bash, so Unix-style paths and rm work fine.
ifeq ($(OS),Windows_NT)
    BINARY := mugi.exe
else
    BINARY := mugi
endif

RUN := ./$(BINARY)

.PHONY: build run demo run-anthropic run-openai run-ollama test test-verbose test-coverage fmt vet clean help

## build: compile the binary
build:
	$(GO) build -o $(BINARY) ./cmd/mugi

## run / demo: build and run with the mock provider (no API key required)
run demo: export LLM_PROVIDER = mock
run demo: build
	$(RUN) $(TASK)

## run-anthropic: run with Anthropic Claude (ANTHROPIC_API_KEY must be set)
run-anthropic: export LLM_PROVIDER = anthropic
run-anthropic: export LLM_MODEL    = claude-sonnet-4-6
run-anthropic: build
	$(RUN) $(TASK)

## run-openai: run with OpenAI (OPENAI_API_KEY must be set)
run-openai: export LLM_PROVIDER = openai
run-openai: export LLM_MODEL    = gpt-4o
run-openai: build
	$(RUN) $(TASK)

## run-ollama: run with a local Ollama model (Ollama must be running)
run-ollama: export LLM_PROVIDER = ollama
run-ollama: export LLM_MODEL    = llama3
run-ollama: build
	$(RUN) $(TASK)

## test: run all tests
test:
	$(GO) test ./...

## test-verbose: run all tests with detailed output
test-verbose:
	$(GO) test -v ./...

## test-coverage: generate an HTML coverage report
test-coverage:
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report written to coverage.html"

## fmt: format all Go source files
fmt:
	$(GO) fmt ./...

## vet: run go vet
vet:
	$(GO) vet ./...

## clean: remove build artifacts and output directory
clean:
	rm -f $(BINARY) coverage.out coverage.html
	rm -rf output/

## help: list available targets
help:
	@grep -E '^##' Makefile | sed 's/## /  /'
