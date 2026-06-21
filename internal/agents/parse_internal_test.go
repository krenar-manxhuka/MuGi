package agents

import (
	"context"
	"strings"
	"testing"

	"mugi/internal/llm"
	"mugi/internal/models"
)

// seqProvider returns a fixed sequence of full responses (content + stop reason),
// one per call, repeating the last once exhausted.
type seqProvider struct {
	responses []llm.Response
	calls     int
}

func (s *seqProvider) Name() string { return "seq" }

func (s *seqProvider) Generate(_ context.Context, _ llm.Request) (llm.Response, error) {
	i := s.calls
	s.calls++
	if i >= len(s.responses) {
		i = len(s.responses) - 1
	}
	return s.responses[i], nil
}

func genFrom(p llm.Provider) func(context.Context) (llm.Response, error) {
	return func(ctx context.Context) (llm.Response, error) { return p.Generate(ctx, llm.Request{}) }
}

const goodArtifact = `{"summary":"s","revision":1,"files":[{"path":"main.go","lang":"go","content":"package main"}]}`

func TestGenerateStructuredSuccessFirstTry(t *testing.T) {
	p := &seqProvider{responses: []llm.Response{{Content: goodArtifact}}}
	art, err := generateStructured[models.Artifact, *models.Artifact](context.Background(), "coder", 2, genFrom(p))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(art.Files) != 1 || p.calls != 1 {
		t.Fatalf("want 1 file in 1 call, got files=%d calls=%d", len(art.Files), p.calls)
	}
}

func TestGenerateStructuredRetriesMalformedThenSucceeds(t *testing.T) {
	p := &seqProvider{responses: []llm.Response{{Content: `{ not json`}, {Content: goodArtifact}}}
	art, err := generateStructured[models.Artifact, *models.Artifact](context.Background(), "coder", 2, genFrom(p))
	if err != nil {
		t.Fatalf("expected recovery on retry, got: %v", err)
	}
	if art == nil || p.calls != 2 {
		t.Fatalf("want recovery on 2nd attempt, calls=%d", p.calls)
	}
}

func TestGenerateStructuredRejectsSemanticallyInvalid(t *testing.T) {
	// Well-formed JSON, but an artifact with no files fails Validate().
	noFiles := `{"summary":"s","revision":1,"files":[]}`
	p := &seqProvider{responses: []llm.Response{{Content: noFiles}}}
	_, err := generateStructured[models.Artifact, *models.Artifact](context.Background(), "coder", 2, genFrom(p))
	if err == nil {
		t.Fatal("expected validation error for empty artifact")
	}
	if p.calls != 2 {
		t.Fatalf("validation failure should be retried; calls=%d", p.calls)
	}
}

func TestGenerateStructuredStopsOnTruncation(t *testing.T) {
	// A truncated response must NOT be retried (a resample just truncates again).
	p := &seqProvider{responses: []llm.Response{{Content: "```json\n{ trunc", StopReason: "max_tokens"}}}
	_, err := generateStructured[models.Artifact, *models.Artifact](context.Background(), "coder", 3, genFrom(p))
	if err == nil {
		t.Fatal("expected error on truncated response")
	}
	if p.calls != 1 {
		t.Fatalf("truncation must short-circuit retry; calls=%d", p.calls)
	}
	if !strings.Contains(err.Error(), "MaxTokens") {
		t.Fatalf("error should carry the truncation hint, got: %v", err)
	}
}
