package index

import (
	"context"
	"sort"
	"strconv"
)

// HybridIndex fuses the rankings of several indexes (typically lexical + semantic)
// with Reciprocal Rank Fusion (RRF). RRF needs no score calibration between the
// parts — only their rank orders — which is exactly why it is the robust default
// for combining keyword and embedding retrieval, where the score scales differ.
type HybridIndex struct {
	parts []Index
	pool  int // candidates pulled from each part before fusing (default 50)
}

// NewHybridIndex fuses the given indexes. pool ≤ 0 uses a sensible default.
func NewHybridIndex(pool int, parts ...Index) *HybridIndex {
	if pool <= 0 {
		pool = 50
	}
	return &HybridIndex{parts: parts, pool: pool}
}

// Retrieve runs each part, fuses by RRF, and returns the top-k fused chunks.
func (h *HybridIndex) Retrieve(ctx context.Context, query string, k int) ([]Chunk, error) {
	if k <= 0 || len(h.parts) == 0 {
		return nil, nil
	}
	const rrfK = 60.0 // standard RRF damping constant

	score := map[string]float64{}
	chunkFor := map[string]Chunk{}
	for _, idx := range h.parts {
		res, err := idx.Retrieve(ctx, query, h.pool)
		if err != nil {
			return nil, err
		}
		for rank, c := range res {
			key := c.Path + ":" + strconv.Itoa(c.Start)
			if _, seen := chunkFor[key]; !seen {
				chunkFor[key] = c
			}
			score[key] += 1.0 / (rrfK + float64(rank) + 1.0)
		}
	}

	keys := make([]string, 0, len(score))
	for key := range score {
		keys = append(keys, key)
	}
	// Fully deterministic: by fused score desc, then key asc.
	sort.Slice(keys, func(a, b int) bool {
		if score[keys[a]] != score[keys[b]] {
			return score[keys[a]] > score[keys[b]]
		}
		return keys[a] < keys[b]
	})
	if k > len(keys) {
		k = len(keys)
	}
	out := make([]Chunk, k)
	for i := 0; i < k; i++ {
		out[i] = chunkFor[keys[i]]
	}
	return out, nil
}
