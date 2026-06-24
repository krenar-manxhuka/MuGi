// Command index builds a lexical retrieval index over a local directory and
// prints the top-k chunks for a query. It is fully offline ($0, no API key, no
// network) — a demonstration of the retrieval core (M1). Point it at any repo,
// including this one:
//
//	go run ./cmd/index -dir . -q "bm25 lexical retrieval" -k 5
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"mugi/internal/index"
)

func main() {
	dir := flag.String("dir", ".", "directory to index")
	query := flag.String("q", "", "query to retrieve for (required)")
	k := flag.Int("k", 5, "number of chunks to return")
	window := flag.Int("window", 50, "chunk size in lines")
	overlap := flag.Int("overlap", 10, "overlap between chunks in lines")
	flag.Parse()

	if strings.TrimSpace(*query) == "" {
		fmt.Fprintln(os.Stderr, "index: -q query is required")
		flag.Usage()
		os.Exit(2)
	}

	chunks, err := index.WindowChunker{WindowLines: *window, OverlapLines: *overlap}.Chunk(*dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "index: chunk %s: %v\n", *dir, err)
		os.Exit(1)
	}

	ix := index.NewLexicalIndex(chunks)
	hits, err := ix.Retrieve(context.Background(), *query, *k)
	if err != nil {
		fmt.Fprintf(os.Stderr, "index: retrieve: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "indexed %d chunks under %q · top %d for %q:\n\n", len(chunks), *dir, len(hits), *query)
	for i, c := range hits {
		fmt.Printf("%2d. %s:%d-%d\n    %s\n", i+1, c.Path, c.Start, c.End, firstLine(c.Content))
	}
	if len(hits) == 0 {
		fmt.Println("(no matching chunks)")
	}
}

// firstLine returns the first non-blank line of a chunk, trimmed, for a preview.
func firstLine(s string) string {
	for _, ln := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(ln); t != "" {
			if len(t) > 100 {
				t = t[:100] + "…"
			}
			return t
		}
	}
	return ""
}
