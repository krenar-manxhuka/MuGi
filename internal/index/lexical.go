package index

import (
	"context"
	"math"
	"sort"
	"strings"
	"unicode"
)

// Index ranks chunks against a query. LexicalIndex implements it today; an
// embedding-backed index will implement the same interface for semantic and
// hybrid retrieval (M2), so callers and the eval flow don't change.
type Index interface {
	Retrieve(ctx context.Context, query string, k int) ([]Chunk, error)
}

// LexicalIndex is a BM25 keyword index over chunks. BM25 is a strong, free
// baseline for code search, especially with an identifier-aware tokenizer
// (so a query for "ParseError" matches code containing parseError / parse_error).
type LexicalIndex struct {
	chunks []Chunk
	tf     []map[string]int // term frequency per chunk
	docLen []float64        // token count per chunk
	df     map[string]int   // document frequency per term
	n      int
	avgLen float64
}

// NewLexicalIndex builds an index over the given chunks.
func NewLexicalIndex(chunks []Chunk) *LexicalIndex {
	ix := &LexicalIndex{chunks: chunks, df: map[string]int{}, n: len(chunks)}
	var total float64
	for _, c := range chunks {
		toks := tokenize(c.Content)
		tf := make(map[string]int, len(toks))
		for _, t := range toks {
			tf[t]++
		}
		for t := range tf {
			ix.df[t]++
		}
		ix.tf = append(ix.tf, tf)
		ix.docLen = append(ix.docLen, float64(len(toks)))
		total += float64(len(toks))
	}
	if ix.n > 0 {
		ix.avgLen = total / float64(ix.n)
	}
	return ix
}

// Retrieve returns the top-k chunks for the query by BM25 score, highest first.
// Chunks that match no query term are excluded. The context is accepted for
// interface parity with embedding-backed indexes; lexical scoring ignores it.
func (ix *LexicalIndex) Retrieve(_ context.Context, query string, k int) ([]Chunk, error) {
	if k <= 0 || ix.n == 0 {
		return nil, nil
	}
	qterms := uniqueTerms(tokenize(query))

	type scored struct {
		idx   int
		score float64
	}
	hits := make([]scored, 0, ix.n)
	for i := range ix.chunks {
		if s := ix.score(qterms, i); s > 0 {
			hits = append(hits, scored{i, s})
		}
	}
	// Stable sort by score desc, then by (path, start) for deterministic ties.
	sort.SliceStable(hits, func(a, b int) bool {
		if hits[a].score != hits[b].score {
			return hits[a].score > hits[b].score
		}
		ca, cb := ix.chunks[hits[a].idx], ix.chunks[hits[b].idx]
		if ca.Path != cb.Path {
			return ca.Path < cb.Path
		}
		return ca.Start < cb.Start
	})
	if k > len(hits) {
		k = len(hits)
	}
	out := make([]Chunk, k)
	for i := 0; i < k; i++ {
		out[i] = ix.chunks[hits[i].idx]
	}
	return out, nil
}

// score is the BM25 score of chunk i against the (unique) query terms.
func (ix *LexicalIndex) score(qterms []string, i int) float64 {
	const k1, b = 1.5, 0.75
	tf := ix.tf[i]
	dl := ix.docLen[i]
	var s float64
	for _, t := range qterms {
		f := float64(tf[t])
		if f == 0 {
			continue
		}
		df := float64(ix.df[t])
		idf := math.Log(1 + (float64(ix.n)-df+0.5)/(df+0.5))
		s += idf * (f * (k1 + 1)) / (f + k1*(1-b+b*dl/ix.avgLen))
	}
	return s
}

func uniqueTerms(toks []string) []string {
	seen := make(map[string]bool, len(toks))
	out := toks[:0:0]
	for _, t := range toks {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// tokenize lowercases and splits text into terms, and additionally splits
// identifiers on camelCase / snake / letter-digit boundaries. For an identifier
// it emits both the whole token and its parts, so "FizzBuzz" yields
// {"fizzbuzz","fizz","buzz"} and "parse_error" yields {"parse","error"} — which
// is what makes keyword search work on code, where queries and identifiers rarely
// share exact surface forms.
func tokenize(text string) []string {
	var out []string
	var cur []rune
	flush := func() {
		if len(cur) == 0 {
			return
		}
		raw := string(cur)
		cur = cur[:0]
		parts := splitIdentifier(raw)
		if len(parts) > 1 {
			out = append(out, strings.ToLower(raw))
		}
		for _, p := range parts {
			if p != "" {
				out = append(out, strings.ToLower(p))
			}
		}
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur = append(cur, r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

// splitIdentifier breaks an identifier at case and letter/digit boundaries,
// handling acronyms ("HTTPServer" -> "HTTP","Server").
func splitIdentifier(s string) []string {
	r := []rune(s)
	if len(r) < 2 {
		return []string{s}
	}
	var parts []string
	start := 0
	for i := 1; i < len(r); i++ {
		prev, cur := r[i-1], r[i]
		boundary := false
		switch {
		case unicode.IsLower(prev) && unicode.IsUpper(cur):
			boundary = true // fooBar -> foo|Bar
		case unicode.IsUpper(prev) && unicode.IsUpper(cur) && i+1 < len(r) && unicode.IsLower(r[i+1]):
			boundary = true // HTTPServer -> HTTP|Server
		case unicode.IsDigit(prev) != unicode.IsDigit(cur):
			boundary = true // foo2bar -> foo|2|bar
		}
		if boundary {
			parts = append(parts, string(r[start:i]))
			start = i
		}
	}
	return append(parts, string(r[start:]))
}
