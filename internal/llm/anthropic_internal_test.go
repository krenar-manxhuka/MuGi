package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newTestProvider returns a provider pointed at the given test server URL with
// near-zero backoff so retry tests run fast.
func newTestProvider(url string) *AnthropicProvider {
	p := NewAnthropicProvider("test-key", "claude-haiku-4-5")
	p.apiURL = url
	p.backoffBase = time.Millisecond
	return p
}

const okBody = `{"content":[{"text":"hello"}],"usage":{"input_tokens":3,"output_tokens":1}}`

func TestAnthropicRetriesOn429ThenSucceeds(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"type":"rate_limit_error","message":"slow down"}}`))
			return
		}
		_, _ = w.Write([]byte(okBody))
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	resp, err := p.Generate(context.Background(), Request{Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if resp.Content != "hello" {
		t.Fatalf("unexpected content: %q", resp.Content)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("expected 3 attempts, got %d", got)
	}
}

func TestAnthropicRetriesOn5xx(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("upstream unavailable")) // non-JSON body on purpose
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	_, err := p.Generate(context.Background(), Request{Messages: []Message{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	// 1 initial + maxRetries attempts.
	if got, want := atomic.LoadInt32(&calls), int32(1+defaultMaxRetries); got != want {
		t.Fatalf("expected %d attempts, got %d", want, got)
	}
	if !strings.Contains(err.Error(), "upstream unavailable") {
		t.Fatalf("error should surface the raw body, got: %v", err)
	}
}

func TestAnthropicBillingErrorFailsFast(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"Your credit balance is too low"}}`))
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	_, err := p.Generate(context.Background(), Request{Messages: []Message{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatal("expected billing error")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("billing error must not be retried; expected 1 attempt, got %d", got)
	}
	if !strings.Contains(err.Error(), "credit balance is too low") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAnthropicRespectsContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"type":"rate_limit_error","message":"slow down"}}`))
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	p.backoffBase = time.Hour // force the wait so cancellation is what unblocks us

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := p.Generate(ctx, Request{Messages: []Message{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatal("expected context error")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("cancellation should interrupt backoff promptly, took %v", time.Since(start))
	}
}

func TestAnthropicCapturesStopReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"content":[{"text":"partial"}],"stop_reason":"max_tokens","usage":{"input_tokens":3,"output_tokens":4096}}`))
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	resp, err := p.Generate(context.Background(), Request{Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StopReason != "max_tokens" {
		t.Fatalf("expected stop_reason max_tokens, got %q", resp.StopReason)
	}
	if !resp.Truncated() {
		t.Fatal("expected Truncated() to be true for a max_tokens stop reason")
	}
}

func TestParseRetryAfter(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "7")
	if got := parseRetryAfter(h); got != 7*time.Second {
		t.Fatalf("expected 7s, got %v", got)
	}
	if got := parseRetryAfter(http.Header{}); got != 0 {
		t.Fatalf("expected 0 for missing header, got %v", got)
	}
	hd := http.Header{}
	hd.Set("Retry-After", "Wed, 21 Oct 2026 07:28:00 GMT") // date form ignored
	if got := parseRetryAfter(hd); got != 0 {
		t.Fatalf("expected 0 for date form, got %v", got)
	}
}
