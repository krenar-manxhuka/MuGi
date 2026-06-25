GO := go

.PHONY: build test test-race vet fmt ci recall predict clean help

## build: compile everything
build:
	$(GO) build ./...

## test: run all tests
test:
	$(GO) test ./...

## test-race: run all tests with the race detector (as CI does)
test-race:
	$(GO) test -race ./...

## vet: run go vet
vet:
	$(GO) vet ./...

## fmt: format all Go source files
fmt:
	$(GO) fmt ./...

## ci: the same gates CI runs
ci: vet build test-race

## recall: measure retrieval recall@k on a slice ($0, no key). SLICE=slice.jsonl
recall:
	$(GO) run ./cmd/swebench-recall -instances $(SLICE) -modes lexical -k 5,10,20

## predict: generate predictions (mock = $0; set LLM_PROVIDER=anthropic to spend). SLICE=slice.jsonl
predict:
	$(GO) run ./cmd/swebench-predict -instances $(SLICE) -context retrieval -k 20

## clean: remove local run artifacts
clean:
	rm -f coverage.out coverage.html recall.json predictions.jsonl slice.jsonl .embed-cache.json

## help: list available targets
help:
	@grep -E '^##' Makefile | sed 's/## /  /'
