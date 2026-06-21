package agents

import (
	"go/format"
	"strings"

	"mugi/internal/models"
)

// formatGoSources canonicalises every Go file in the artifact with gofmt
// (in-process via go/format — no external binary). A file that fails to parse is
// left untouched; the build step will surface the syntax error. This guarantees
// the artifact we store, review, build, and write out is always gofmt-clean,
// which is part of "produces good stuff" for the MVP.
func formatGoSources(a *models.Artifact) {
	for i := range a.Files {
		f := &a.Files[i]
		if !isGoSource(*f) {
			continue
		}
		if out, err := format.Source([]byte(f.Content)); err == nil {
			f.Content = string(out)
		}
	}
}

func isGoSource(f models.File) bool {
	return strings.EqualFold(f.Lang, "go") || strings.HasSuffix(f.Path, ".go")
}
