package index

import (
	"context"
	"reflect"
	"testing"
)

func TestSplitIdentifier(t *testing.T) {
	cases := map[string][]string{
		"FizzBuzz":   {"Fizz", "Buzz"},
		"HTTPServer": {"HTTP", "Server"},
		"parse":      {"parse"},
		"foo2bar":    {"foo", "2", "bar"},
		"x":          {"x"},
	}
	for in, want := range cases {
		if got := splitIdentifier(in); !reflect.DeepEqual(got, want) {
			t.Errorf("splitIdentifier(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestTokenizeSplitsIdentifiers(t *testing.T) {
	got := map[string]bool{}
	for _, tok := range tokenize("func FizzBuzz() and parse_error") {
		got[tok] = true
	}
	for _, want := range []string{"fizzbuzz", "fizz", "buzz", "parse", "error", "func"} {
		if !got[want] {
			t.Errorf("expected token %q in %v", want, got)
		}
	}
}

func TestLexicalRetrieve(t *testing.T) {
	chunks := []Chunk{
		{Path: "fizz.go", Start: 1, End: 3, Content: "func FizzBuzz(n int) []string { return fizzbuzz logic }"},
		{Path: "rev.go", Start: 1, End: 3, Content: "func Reverse(s string) string { reverse the runes }"},
		{Path: "http.go", Start: 1, End: 3, Content: "func healthHandler() { start an http server }"},
	}
	ix := NewLexicalIndex(chunks)

	got, err := ix.Retrieve(context.Background(), "fizzbuzz implementation", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != "fizz.go" {
		t.Fatalf("top hit = %+v, want fizz.go", got)
	}

	// The identifier-aware tokenizer should let a camelCase query find snake/camel code.
	got, _ = ix.Retrieve(context.Background(), "HTTP health server", 1)
	if len(got) != 1 || got[0].Path != "http.go" {
		t.Fatalf("top hit = %+v, want http.go", got)
	}

	// k larger than the corpus, and a no-match query, are both well-behaved.
	if all, _ := ix.Retrieve(context.Background(), "reverse", 100); len(all) < 1 {
		t.Fatal("expected at least one hit for 'reverse'")
	}
	if none, _ := ix.Retrieve(context.Background(), "zzzznonexistent", 5); len(none) != 0 {
		t.Fatalf("expected no hits, got %d", len(none))
	}
}
