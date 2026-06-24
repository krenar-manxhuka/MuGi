package index

import (
	"context"
	"crypto/sha256"
	"math"
)

// Embedder maps texts to dense vectors. It mirrors the llm.Provider seam: real
// implementations call an embeddings API; MockEmbedder is deterministic so the
// whole index/retrieval pipeline is unit-testable offline with no network. Real
// embedders and hybrid (lexical+semantic) retrieval are M2 — the interface is
// defined now so that lands without reshaping callers.
type Embedder interface {
	// Embed returns one unit-length vector per input text, in order.
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	// Name identifies the embedding model, for provenance.
	Name() string
}

// MockEmbedder produces deterministic, content-derived unit vectors. Identical
// text always yields the identical vector (so runs are reproducible and tests are
// stable); different text yields a different vector. It carries no semantic
// meaning — it exists to exercise the seam, not to retrieve well.
type MockEmbedder struct {
	Dim int // vector dimension (default 64)
}

func (m MockEmbedder) Name() string { return "mock-embedder" }

// Embed hashes each text and expands the digest into a deterministic unit vector.
func (m MockEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	dim := m.Dim
	if dim <= 0 {
		dim = 64
	}
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = unitVector(t, dim)
	}
	return out, nil
}

func unitVector(text string, dim int) []float32 {
	v := make([]float32, dim)
	// Stretch the 32-byte digest to `dim` values by re-hashing with a counter,
	// so dimensions beyond 32 are still deterministic and well-distributed.
	for j := 0; j < dim; j++ {
		h := sha256.Sum256([]byte(text + "#" + string(rune(j))))
		v[j] = float32(int16(uint16(h[0])<<8|uint16(h[1]))) / 32768.0 // in [-1,1)
	}
	return normalize(v)
}

// normalize scales v to unit length in place and returns it (a zero vector is
// returned unchanged). Unit-length embeddings make cosine similarity a dot
// product and keep the mock and real embedders on the same footing.
func normalize(v []float32) []float32 {
	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	if norm == 0 {
		return v
	}
	inv := float32(1 / math.Sqrt(norm))
	for i := range v {
		v[i] *= inv
	}
	return v
}
