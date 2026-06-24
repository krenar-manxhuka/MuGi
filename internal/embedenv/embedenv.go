// Package embedenv builds an embeddings client from the environment so the
// retrieval commands share one place that knows the variables, the caching
// wrapper, and where the cache file lives — instead of each command re-deriving
// it. The defaults point at OpenAI, but setting EMBED_BASE_URL at a local Ollama
// endpoint makes embeddings free and offline.
package embedenv

import (
	"fmt"
	"io"
	"os"
	"time"

	"mugi/internal/index"
)

// Embedder is an environment-configured embedder plus the cache backing it. It
// satisfies index.Embedder (Name/Embed are promoted from the wrapped caching
// embedder); Save persists the cache so a later run does not re-pay.
type Embedder struct {
	index.Embedder
	cache     *index.MemoryCache
	cachePath string
	warnw     io.Writer
}

// FromEnv constructs an OpenAI-compatible embedder wrapped in the content-addressed
// cache. Non-fatal warnings (e.g. an unreadable cache) go to warnw; pass nil to
// discard them. The variables read:
//
//	EMBED_API_KEY / OPENAI_API_KEY  bearer token ("" for a keyless local endpoint)
//	EMBED_BASE_URL                  endpoint, default https://api.openai.com/v1
//	EMBED_MODEL                     model, default text-embedding-3-small
//	EMBED_CACHE                     cache file, default .embed-cache.json
func FromEnv(warnw io.Writer) Embedder {
	if warnw == nil {
		warnw = io.Discard
	}
	apiKey := os.Getenv("EMBED_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	baseURL := env("EMBED_BASE_URL", "https://api.openai.com/v1")
	model := env("EMBED_MODEL", "text-embedding-3-small")
	cachePath := env("EMBED_CACHE", ".embed-cache.json")

	cache, err := index.LoadMemoryCache(cachePath)
	if err != nil {
		fmt.Fprintf(warnw, "embedenv: load cache %s: %v\n", cachePath, err)
		cache = index.NewMemoryCache()
	}
	return Embedder{
		Embedder:  index.CachingEmbedder{Inner: index.NewOpenAIEmbedder(baseURL, apiKey, model, 60*time.Second), Cache: cache},
		cache:     cache,
		cachePath: cachePath,
		warnw:     warnw,
	}
}

// Save persists the embedding cache to its file. Call it after a run.
func (e Embedder) Save() {
	if err := e.cache.SaveTo(e.cachePath); err != nil {
		fmt.Fprintf(e.warnw, "embedenv: save cache %s: %v\n", e.cachePath, err)
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
