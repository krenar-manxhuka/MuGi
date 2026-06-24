package index

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"
)

// maxEmbedResponseBytes caps how much of an embeddings response we read, so a
// hostile or broken endpoint can't exhaust memory.
const maxEmbedResponseBytes = 64 << 20

// OpenAIEmbedder calls any OpenAI-compatible `/v1/embeddings` endpoint:
//   - OpenAI (e.g. text-embedding-3-small)
//   - a local Ollama embedding model (set baseURL to the Ollama OpenAI endpoint
//     and use e.g. nomic-embed-text) — runs offline and free
//   - Voyage and other compatible hosts
//
// Pass an empty apiKey for local endpoints that need no auth.
type OpenAIEmbedder struct {
	baseURL  string
	apiKey   string
	model    string
	maxBatch int
	client   *http.Client
}

// NewOpenAIEmbedder builds an embedder. baseURL empty → OpenAI; timeout 0 → 120s.
func NewOpenAIEmbedder(baseURL, apiKey, model string, timeout time.Duration) *OpenAIEmbedder {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if timeout == 0 {
		timeout = 120 * time.Second
	}
	return &OpenAIEmbedder{
		baseURL:  baseURL,
		apiKey:   apiKey,
		model:    model,
		maxBatch: 128,
		client:   &http.Client{Timeout: timeout},
	}
}

func (e *OpenAIEmbedder) Name() string { return "openai/" + e.model }

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Embed returns one unit-length vector per input text, in input order, batching
// large inputs to keep requests bounded. The context governs every HTTP call.
func (e *OpenAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for start := 0; start < len(texts); start += e.maxBatch {
		end := start + e.maxBatch
		if end > len(texts) {
			end = len(texts)
		}
		vecs, err := e.embedBatch(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}
		copy(out[start:end], vecs)
	}
	return out, nil
}

func (e *OpenAIEmbedder) embedBatch(ctx context.Context, batch []string) ([][]float32, error) {
	body, err := json.Marshal(embedRequest{Model: e.model, Input: batch})
	if err != nil {
		return nil, fmt.Errorf("embed: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embed: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed: http: %w", err)
	}
	defer resp.Body.Close()

	var r embedResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxEmbedResponseBytes)).Decode(&r); err != nil {
		return nil, fmt.Errorf("embed: decode response: %w", err)
	}
	if r.Error != nil {
		return nil, fmt.Errorf("embed: api error: %s", r.Error.Message)
	}
	if len(r.Data) != len(batch) {
		return nil, fmt.Errorf("embed: got %d vectors for %d inputs", len(r.Data), len(batch))
	}

	// The API is not required to return data in input order — sort by index.
	sort.Slice(r.Data, func(i, j int) bool { return r.Data[i].Index < r.Data[j].Index })
	out := make([][]float32, len(batch))
	for i := range r.Data {
		if len(r.Data[i].Embedding) == 0 {
			return nil, fmt.Errorf("embed: empty vector at index %d", r.Data[i].Index)
		}
		out[i] = normalize(r.Data[i].Embedding)
	}
	return out, nil
}
