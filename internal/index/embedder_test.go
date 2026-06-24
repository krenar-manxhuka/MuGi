package index

import (
	"context"
	"math"
	"testing"
)

func TestMockEmbedderDeterministicAndUnit(t *testing.T) {
	e := MockEmbedder{Dim: 32}
	ctx := context.Background()

	a, err := e.Embed(ctx, []string{"hello", "world"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := e.Embed(ctx, []string{"hello", "world"})

	// Deterministic: same input -> identical vectors.
	for i := range a {
		if len(a[i]) != 32 {
			t.Fatalf("dim = %d, want 32", len(a[i]))
		}
		for j := range a[i] {
			if a[i][j] != b[i][j] {
				t.Fatal("embeddings are not deterministic")
			}
		}
	}

	// Different input -> different vector.
	if vecEqual(a[0], a[1]) {
		t.Fatal("distinct texts produced identical vectors")
	}

	// Unit length.
	for _, v := range a {
		var norm float64
		for _, x := range v {
			norm += float64(x) * float64(x)
		}
		if math.Abs(math.Sqrt(norm)-1) > 1e-5 {
			t.Fatalf("vector not unit length: |v| = %f", math.Sqrt(norm))
		}
	}
}

func vecEqual(a, b []float32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
