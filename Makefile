BINARY   := mugi
TASK     ?= "Build a minimal Go HTTP server with a /health endpoint and graceful shutdown"
GO       := go

.PHONY: build run demo test test-verbose test-coverage fmt vet clean help

## build: compile the binary
build:
	$(GO) build -o $(BINARY) ./cmd/mugi

## run / demo: build and run with the mock provider (no API key required)
run demo: build
	LLM_PROVIDER=mock ./$(BINARY) $(TASK)

## run-anthropic: run with Anthropic Claude (ANTHROPIC_API_KEY must be set)
run-anthropic: build
	LLM_PROVIDER=anthropic LLM_MODEL=claude-sonnet-4-6 ./$(BINARY) $(TASK)

## run-openai: run with OpenAI (OPENAI_API_KEY must be set)
run-openai: build
	LLM_PROVIDER=openai LLM_MODEL=gpt-4o ./$(BINARY) $(TASK)

## run-ollama: run with a local Ollama model (Ollama must be running)
run-ollama: build
	LLM_PROVIDER=ollama LLM_MODEL=llama3 ./$(BINARY) $(TASK)

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
	@echo "Coverage report: coverage.html"

## fmt: format all Go source files
fmt:
	$(GO) fmt ./...

## vet: run go vet
vet:
	$(GO) vet ./...

## clean: remove build artifacts and output
clean:
	rm -f $(BINARY) coverage.out coverage.html
	rm -rf output/

## help: list available targets
help:
	@grep -E '^##' Makefile | sed 's/## /  /'
