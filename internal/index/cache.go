package index

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync"
)

// Cache stores embedding vectors keyed by a content hash. Because keys are
// content-addressed (model + text), reusing a cache across runs is always safe:
// the same text under the same model yields the same vector.
type Cache interface {
	Get(key string) ([]float32, bool)
	Put(key string, vec []float32)
}

// MemoryCache is a concurrency-safe in-memory Cache with optional JSON
// persistence so embeddings survive across runs (the actual cost saver).
type MemoryCache struct {
	mu sync.RWMutex
	m  map[string][]float32
}

// NewMemoryCache returns an empty cache.
func NewMemoryCache() *MemoryCache { return &MemoryCache{m: map[string][]float32{}} }

func (c *MemoryCache) Get(key string) ([]float32, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.m[key]
	return v, ok
}

func (c *MemoryCache) Put(key string, vec []float32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = vec
}

// SaveTo writes the cache to path as JSON.
func (c *MemoryCache) SaveTo(path string) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	data, err := json.Marshal(c.m)
	if err != nil {
		return fmt.Errorf("cache: marshal: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

// LoadMemoryCache reads a cache previously written by SaveTo. A missing file is
// not an error — it returns an empty cache, so first runs just start cold.
func LoadMemoryCache(path string) (*MemoryCache, error) {
	c := NewMemoryCache()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cache: read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &c.m); err != nil {
		return nil, fmt.Errorf("cache: parse %s: %w", path, err)
	}
	return c, nil
}

// CachingEmbedder wraps an Embedder so already-seen texts are served from a Cache
// instead of re-embedded — making re-indexing cheap and runs reproducible. It
// de-duplicates within a batch too, so repeated chunks cost one embed at most.
type CachingEmbedder struct {
	Inner Embedder
	Cache Cache
}

func (c CachingEmbedder) Name() string { return c.Inner.Name() }

func (c CachingEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	missing := map[string][]int{} // text -> positions in out

	for i, t := range texts {
		if v, ok := c.Cache.Get(c.key(t)); ok {
			out[i] = v
			continue
		}
		missing[t] = append(missing[t], i)
	}
	if len(missing) == 0 {
		return out, nil
	}

	uniq := make([]string, 0, len(missing))
	for t := range missing {
		uniq = append(uniq, t)
	}
	vecs, err := c.Inner.Embed(ctx, uniq)
	if err != nil {
		return nil, err
	}
	if len(vecs) != len(uniq) {
		return nil, fmt.Errorf("cache: inner returned %d vectors for %d inputs", len(vecs), len(uniq))
	}
	for j, t := range uniq {
		c.Cache.Put(c.key(t), vecs[j])
		for _, pos := range missing[t] {
			out[pos] = vecs[j]
		}
	}
	return out, nil
}

// key is the content-addressed cache key: the model name and text, hashed.
// Including the model name prevents vectors from one model serving another.
func (c CachingEmbedder) key(text string) string {
	h := sha256.Sum256([]byte(c.Inner.Name() + "\x00" + text))
	return hex.EncodeToString(h[:])
}
