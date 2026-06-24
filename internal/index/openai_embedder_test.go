package index

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOpenAIEmbedder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			http.Error(w, "wrong path", http.StatusNotFound)
			return
		}
		var req embedRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		// Return vectors deliberately OUT OF ORDER (index 1 before index 0) to
		// prove the client re-orders by index back to input order.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[
			{"index":1,"embedding":[0,3,4]},
			{"index":0,"embedding":[3,0,4]}
		]}`))
	}))
	defer srv.Close()

	e := NewOpenAIEmbedder(srv.URL, "", "test-model", 5*time.Second)
	vecs, err := e.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 {
		t.Fatalf("got %d vectors, want 2", len(vecs))
	}
	// Input "a" (index 0) → {3,0,4}, normalized → {0.6, 0, 0.8}.
	if d := math.Abs(float64(vecs[0][0])-0.6) + math.Abs(float64(vecs[0][2])-0.8); d > 1e-4 {
		t.Fatalf("vec[0] = %v, want ~{0.6,0,0.8} (input order + normalized)", vecs[0])
	}
	if math.Abs(vecLen(vecs[1])-1) > 1e-5 {
		t.Fatalf("vec[1] not unit length: %v", vecs[1])
	}
}

func TestOpenAIEmbedderHonorsContext(t *testing.T) {
	e := NewOpenAIEmbedder("http://127.0.0.1:0", "", "m", time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled
	if _, err := e.Embed(ctx, []string{"x"}); err == nil {
		t.Fatal("expected an error when the context is cancelled")
	}
}

func vecLen(v []float32) float64 {
	var s float64
	for _, x := range v {
		s += float64(x) * float64(x)
	}
	return math.Sqrt(s)
}
