package index

import (
	"context"
	"fmt"
	"math"
	"sort"
)

// SemanticIndex ranks chunks by embedding similarity to the query. It embeds
// every chunk once at construction (via the injected Embedder) and, per query,
// embeds the query and ranks by cosine similarity. With the mock embedder it is
// fully offline; with a real embedder it is the "meaning" half of hybrid search.
type SemanticIndex struct {
	chunks []Chunk
	vecs   [][]float32
	emb    Embedder
}

// NewSemanticIndex embeds the chunks and returns a queryable index. The context
// governs the (network) embedding call, so a slow or cancelled embed fails fast.
func NewSemanticIndex(ctx context.Context, chunks []Chunk, emb Embedder) (*SemanticIndex, error) {
	if emb == nil {
		return nil, fmt.Errorf("index: nil embedder")
	}
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Content
	}
	vecs, err := emb.Embed(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("index: embed chunks: %w", err)
	}
	if len(vecs) != len(chunks) {
		return nil, fmt.Errorf("index: embedder returned %d vectors for %d chunks", len(vecs), len(chunks))
	}
	return &SemanticIndex{chunks: chunks, vecs: vecs, emb: emb}, nil
}

// Retrieve returns the top-k chunks by cosine similarity to the query.
func (s *SemanticIndex) Retrieve(ctx context.Context, query string, k int) ([]Chunk, error) {
	if k <= 0 || len(s.chunks) == 0 {
		return nil, nil
	}
	qv, err := s.emb.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("index: embed query: %w", err)
	}
	if len(qv) != 1 {
		return nil, fmt.Errorf("index: embedder returned %d query vectors, want 1", len(qv))
	}

	type scored struct {
		idx   int
		score float64
	}
	hits := make([]scored, len(s.chunks))
	for i := range s.chunks {
		hits[i] = scored{i, cosine(qv[0], s.vecs[i])}
	}
	sort.SliceStable(hits, func(a, b int) bool {
		if hits[a].score != hits[b].score {
			return hits[a].score > hits[b].score
		}
		ca, cb := s.chunks[hits[a].idx], s.chunks[hits[b].idx]
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
		out[i] = s.chunks[hits[i].idx]
	}
	return out, nil
}

// cosine is the cosine similarity of two equal-length vectors. It does not assume
// unit length (it normalizes), so it is correct even for un-normalized embedders.
func cosine(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
