package index

import (
	"context"
	"path/filepath"
	"testing"
)

// countingEmbedder records how many texts the inner embedder was actually asked
// to embed, so caching/dedup can be asserted.
type countingEmbedder struct {
	inner Embedder
	calls *int
}

func (c countingEmbedder) Name() string { return c.inner.Name() }
func (c countingEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	*c.calls += len(texts)
	return c.inner.Embed(ctx, texts)
}

func TestCachingEmbedderDedupAndReuse(t *testing.T) {
	calls := 0
	ce := CachingEmbedder{
		Inner: countingEmbedder{inner: MockEmbedder{Dim: 16}, calls: &calls},
		Cache: NewMemoryCache(),
	}
	ctx := context.Background()

	// Within a batch, repeats are de-duplicated: {a,b,a} embeds only {a,b}.
	v1, err := ce.Embed(ctx, []string{"a", "b", "a"})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("inner embedded %d texts, want 2 (deduped)", calls)
	}
	if !vecEqual(v1[0], v1[2]) {
		t.Fatal("repeated text must map to the same vector")
	}

	// Across calls, cached texts are not re-embedded.
	if _, err := ce.Embed(ctx, []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("inner embedded %d after an all-cached call, want still 2", calls)
	}

	// A new text costs exactly one more embed.
	if _, err := ce.Embed(ctx, []string{"c"}); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("inner embedded %d, want 3", calls)
	}
}

func TestMemoryCacheSaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")

	c := NewMemoryCache()
	c.Put("k", []float32{1, 2, 3})
	if err := c.SaveTo(path); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadMemoryCache(path)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := loaded.Get("k"); !ok || !vecEqual(v, []float32{1, 2, 3}) {
		t.Fatalf("loaded %v ok=%v, want {1,2,3}", v, ok)
	}

	// A missing cache file is a cold start, not an error.
	empty, err := LoadMemoryCache(filepath.Join(dir, "absent.json"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if _, ok := empty.Get("k"); ok {
		t.Fatal("expected an empty cache for a missing file")
	}
}
