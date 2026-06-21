package agents

import (
	"strings"
	"testing"

	"mugi/internal/models"
)

func TestFormatGoSources(t *testing.T) {
	a := &models.Artifact{Files: []models.File{
		{Path: "main.go", Lang: "go", Content: "package main\nfunc  main(){println( 1 )}\n"},
		{Path: "notes.txt", Lang: "text", Content: "not   go   code"},
		{Path: "broken.go", Lang: "go", Content: "package main\nfunc ("}, // unparseable
	}}

	formatGoSources(a)

	// Valid Go is canonicalised (gofmt collapses "func  main" → "func main").
	if !strings.Contains(a.Files[0].Content, "func main()") {
		t.Fatalf("main.go was not gofmt-formatted: %q", a.Files[0].Content)
	}
	// Non-Go files are left untouched.
	if a.Files[1].Content != "not   go   code" {
		t.Fatalf("non-Go file was altered: %q", a.Files[1].Content)
	}
	// Unparseable Go is left as-is so the build step reports the real error.
	if a.Files[2].Content != "package main\nfunc (" {
		t.Fatalf("invalid Go was altered instead of left for the compiler: %q", a.Files[2].Content)
	}
}
