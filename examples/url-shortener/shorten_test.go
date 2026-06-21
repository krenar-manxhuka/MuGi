package shorten_test

import (
	"fmt"
	"sync"
	"testing"

	"example.com/shorten"
)

func TestRoundTrip(t *testing.T) {
	s := shorten.NewStore()
	const longURL = "https://www.example.com/some/very/long/path?query=1"

	code, err := s.Shorten(longURL)
	if err != nil {
		t.Fatalf("Shorten returned unexpected error: %v", err)
	}
	if len(code) != 6 {
		t.Fatalf("expected code of length 6, got %d (%q)", len(code), code)
	}

	resolved, ok := s.Resolve(code)
	if !ok {
		t.Fatalf("Resolve(%q) returned ok=false, expected true", code)
	}
	if resolved != longURL {
		t.Fatalf("Resolve(%q) = %q, want %q", code, resolved, longURL)
	}
}

func TestIdempotency(t *testing.T) {
	s := shorten.NewStore()
	const longURL = "https://www.example.com/idempotency-test"

	code1, err := s.Shorten(longURL)
	if err != nil {
		t.Fatalf("first Shorten returned unexpected error: %v", err)
	}

	code2, err := s.Shorten(longURL)
	if err != nil {
		t.Fatalf("second Shorten returned unexpected error: %v", err)
	}

	if code1 != code2 {
		t.Fatalf("idempotency violated: first code %q, second code %q", code1, code2)
	}
}

func TestDistinctness(t *testing.T) {
	s := shorten.NewStore()

	code1, err := s.Shorten("https://www.example.com/page-one")
	if err != nil {
		t.Fatalf("Shorten #1 returned unexpected error: %v", err)
	}

	code2, err := s.Shorten("https://www.example.com/page-two")
	if err != nil {
		t.Fatalf("Shorten #2 returned unexpected error: %v", err)
	}

	if code1 == code2 {
		t.Fatalf("distinctness violated: both URLs returned the same code %q", code1)
	}
}

func TestResolveMissing(t *testing.T) {
	s := shorten.NewStore()

	resolved, ok := s.Resolve("XXXXXX")
	if ok {
		t.Fatalf("Resolve of missing code returned ok=true with value %q", resolved)
	}
	if resolved != "" {
		t.Fatalf("Resolve of missing code returned non-empty string: %q", resolved)
	}
}

func TestShortenEmpty(t *testing.T) {
	s := shorten.NewStore()

	code, err := s.Shorten("")
	if err == nil {
		t.Fatalf("Shorten(\"\") expected a non-nil error, got code %q", code)
	}
}

func TestConcurrent(t *testing.T) {
	const numGoroutines = 100
	s := shorten.NewStore()

	type result struct {
		url  string
		code string
		err  error
	}

	results := make([]result, numGoroutines)
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			url := fmt.Sprintf("https://example.com/page/%d", i)
			code, err := s.Shorten(url)
			results[i] = result{url: url, code: code, err: err}
		}()
	}

	wg.Wait()

	// Verify all Shorten calls succeeded and all codes round-trip.
	seenCodes := make(map[string]string) // code -> url
	for i, r := range results {
		if r.err != nil {
			t.Errorf("goroutine %d: Shorten(%q) returned error: %v", i, r.url, r.err)
			continue
		}
		if len(r.code) != 6 {
			t.Errorf("goroutine %d: expected code length 6, got %d (%q)", i, len(r.code), r.code)
			continue
		}

		// Check for unexpected code collisions across distinct URLs.
		if existing, collision := seenCodes[r.code]; collision && existing != r.url {
			t.Errorf("code collision: code %q assigned to both %q and %q", r.code, existing, r.url)
		}
		seenCodes[r.code] = r.url

		// Verify round-trip.
		resolved, ok := s.Resolve(r.code)
		if !ok {
			t.Errorf("goroutine %d: Resolve(%q) returned ok=false", i, r.code)
			continue
		}
		if resolved != r.url {
			t.Errorf("goroutine %d: Resolve(%q) = %q, want %q", i, r.code, resolved, r.url)
		}
	}
}
