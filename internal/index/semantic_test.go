package index

import (
	"context"
	"math"
	"testing"
)

func TestSemanticIndexRetrieve(t *testing.T) {
	chunks := []Chunk{
		{Path: "a.go", Start: 1, End: 2, Content: "alpha apple ant"},
		{Path: "b.go", Start: 1, End: 2, Content: "beta banana bear"},
		{Path: "c.go", Start: 1, End: 2, Content: "gamma grape goat"},
	}
	ix, err := NewSemanticIndex(context.Background(), chunks, MockEmbedder{Dim: 64})
	if err != nil {
		t.Fatal(err)
	}

	// A query identical to a chunk embeds to the same vector (cosine 1), so that
	// chunk must rank first — a deterministic check of the ranking mechanism.
	got, err := ix.Retrieve(context.Background(), "beta banana bear", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != "b.go" {
		t.Fatalf("top hit = %+v, want b.go", got)
	}

	if all, _ := ix.Retrieve(context.Background(), "alpha apple ant", 10); len(all) != 3 {
		t.Fatalf("k>corpus should return all 3, got %d", len(all))
	}
}

func TestCosine(t *testing.T) {
	if c := cosine([]float32{1, 0}, []float32{1, 0}); math.Abs(c-1) > 1e-9 {
		t.Errorf("parallel cosine = %f, want 1", c)
	}
	if c := cosine([]float32{1, 0}, []float32{0, 1}); math.Abs(c) > 1e-9 {
		t.Errorf("orthogonal cosine = %f, want 0", c)
	}
	if c := cosine([]float32{1, 0}, []float32{0, 0}); c != 0 {
		t.Errorf("zero-vector cosine = %f, want 0", c)
	}
}
