// Command index builds a retrieval index over a local directory and prints the
// top-k chunks for a query.
//
//	# lexical (BM25) — fully offline, $0, no key:
//	go run ./cmd/index -dir . -q "bm25 lexical retrieval" -k 5
//
//	# semantic / hybrid — needs an embeddings endpoint. Point EMBED_BASE_URL at a
//	# local Ollama server for a free, offline embedder, or set EMBED_API_KEY for a
//	# hosted one. Embeddings are cached (EMBED_CACHE) so reruns don't re-pay.
//	EMBED_BASE_URL=http://localhost:11434/v1 EMBED_MODEL=nomic-embed-text \
//	  go run ./cmd/index -mode hybrid -q "graceful shutdown" -k 5
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"mugi/internal/embedenv"
	"mugi/internal/index"
)

func main() {
	dir := flag.String("dir", ".", "directory to index")
	query := flag.String("q", "", "query to retrieve for (required)")
	k := flag.Int("k", 5, "number of chunks to return")
	window := flag.Int("window", 50, "chunk size in lines")
	overlap := flag.Int("overlap", 10, "overlap between chunks in lines")
	mode := flag.String("mode", "lexical", "retrieval mode: lexical | semantic | hybrid")
	flag.Parse()

	if strings.TrimSpace(*query) == "" {
		fmt.Fprintln(os.Stderr, "index: -q query is required")
		flag.Usage()
		os.Exit(2)
	}

	ctx := context.Background()
	chunks, err := index.WindowChunker{WindowLines: *window, OverlapLines: *overlap}.Chunk(*dir)
	if err != nil {
		fail("chunk %s: %v", *dir, err)
	}

	ix, cleanup, err := buildIndex(ctx, *mode, chunks)
	if err != nil {
		fail("%v", err)
	}
	defer cleanup()

	hits, err := ix.Retrieve(ctx, *query, *k)
	if err != nil {
		fail("retrieve: %v", err)
	}

	fmt.Fprintf(os.Stderr, "indexed %d chunks under %q · mode=%s · top %d for %q:\n\n",
		len(chunks), *dir, *mode, len(hits), *query)
	for i, c := range hits {
		fmt.Printf("%2d. %s:%d-%d\n    %s\n", i+1, c.Path, c.Start, c.End, firstLine(c.Content))
	}
	if len(hits) == 0 {
		fmt.Println("(no matching chunks)")
	}
}

// buildIndex constructs the requested index. lexical is fully offline; semantic
// and hybrid embed the chunks via an endpoint configured from the environment.
// cleanup persists the embedding cache.
func buildIndex(ctx context.Context, mode string, chunks []index.Chunk) (index.Index, func(), error) {
	noop := func() {}
	switch mode {
	case "lexical":
		return index.NewLexicalIndex(chunks), noop, nil
	case "semantic", "hybrid":
		emb := embedenv.FromEnv(os.Stderr)
		sem, err := index.NewSemanticIndex(ctx, chunks, emb)
		if err != nil {
			return nil, noop, fmt.Errorf("%s index: %w\n(configure EMBED_BASE_URL/EMBED_API_KEY/EMBED_MODEL, or use -mode lexical)", mode, err)
		}
		if mode == "semantic" {
			return sem, emb.Save, nil
		}
		return index.NewHybridIndex(50, index.NewLexicalIndex(chunks), sem), emb.Save, nil
	default:
		return nil, noop, fmt.Errorf("unknown -mode %q (want lexical|semantic|hybrid)", mode)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "index: "+format+"\n", args...)
	os.Exit(1)
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
