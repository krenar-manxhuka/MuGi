package index

import (
	"context"
	"testing"
)

// stubIndex returns a fixed ranking, so RRF fusion can be tested deterministically
// without depending on any real retriever.
type stubIndex struct{ res []Chunk }

func (s stubIndex) Retrieve(_ context.Context, _ string, k int) ([]Chunk, error) {
	if k > len(s.res) {
		k = len(s.res)
	}
	return s.res[:k], nil
}

func TestHybridRRFFusion(t *testing.T) {
	c1 := Chunk{Path: "1", Start: 1}
	c2 := Chunk{Path: "2", Start: 1}
	c3 := Chunk{Path: "3", Start: 1}

	// Part A ranks c1,c2,c3; part B ranks c3,c1,c2.
	// RRF (k=60): c1 = 1/61+1/62 > c3 = 1/63+1/61 > c2 = 1/62+1/63.
	h := NewHybridIndex(10, stubIndex{[]Chunk{c1, c2, c3}}, stubIndex{[]Chunk{c3, c1, c2}})

	got, err := h.Retrieve(context.Background(), "q", 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"1", "3", "2"}
	if len(got) != 3 {
		t.Fatalf("got %d results, want 3", len(got))
	}
	for i, w := range want {
		if got[i].Path != w {
			t.Fatalf("fused order = %v, want %v", paths(got), want)
		}
	}
}

func paths(cs []Chunk) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Path
	}
	return out
}
