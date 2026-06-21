package agents

import (
	"encoding/json"
	"testing"

	"mugi/internal/models"
)

// TestRepairJSONStringsLeavesValidJSONUnchanged verifies the repair pass is a
// no-op on already-valid JSON: it must never alter compliant model output.
func TestRepairJSONStringsLeavesValidJSONUnchanged(t *testing.T) {
	valid := `{"content":"package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n","n":2}`
	if got := repairJSONStrings(valid); got != valid {
		t.Fatalf("repair altered valid JSON:\n in: %s\nout: %s", valid, got)
	}
}

// TestRepairJSONStringsEscapesRawControlChars reproduces the real qwen2.5-coder
// failure mode: source code pasted into a "content" field with literal newlines
// and tabs instead of \n and \t. After repair the result must be valid JSON
// whose decoded value is byte-identical to the model's intent.
func TestRepairJSONStringsEscapesRawControlChars(t *testing.T) {
	const code = "package fib\n\nimport \"fmt\"\n\nfunc Fib(n int) int {\n\treturn n\n}\n"
	// Build broken JSON: a real (unescaped) newline/tab body, but inner quotes
	// already escaped — exactly what the model produced.
	broken := "{\n  \"path\": \"fib.go\",\n  \"content\": \"" +
		"package fib\n\nimport \\\"fmt\\\"\n\nfunc Fib(n int) int {\n\treturn n\n}\n" +
		"\"\n}"

	repaired := repairJSONStrings(broken)

	var f models.File
	if err := json.Unmarshal([]byte(repaired), &f); err != nil {
		t.Fatalf("repaired JSON still does not parse: %v\nrepaired: %s", err, repaired)
	}
	if f.Content != code {
		t.Fatalf("decoded content mismatch:\nwant %q\ngot  %q", code, f.Content)
	}
}

// TestRepairJSONStringsEscapesOtherControlChars covers carriage returns and a
// bare control byte, which must become \r and a \u escape respectively.
func TestRepairJSONStringsEscapesOtherControlChars(t *testing.T) {
	broken := "{\"v\":\"a\rb\x01c\"}"
	repaired := repairJSONStrings(broken)

	var out struct {
		V string `json:"v"`
	}
	if err := json.Unmarshal([]byte(repaired), &out); err != nil {
		t.Fatalf("repaired JSON does not parse: %v\nrepaired: %q", err, repaired)
	}
	if out.V != "a\rb\x01c" {
		t.Fatalf("decoded value mismatch: got %q", out.V)
	}
}

// TestExtractJSONStripsFenceAndRepairs verifies extractJSON composes both
// behaviours: it removes a ```json fence and repairs raw control chars in one
// pass, yielding parseable JSON.
func TestExtractJSONStripsFenceAndRepairs(t *testing.T) {
	wrapped := "```json\n{\n  \"content\": \"line1\nline2\"\n}\n```"
	got := extractJSON(wrapped)

	var f models.File
	if err := json.Unmarshal([]byte(got), &f); err != nil {
		t.Fatalf("extractJSON output does not parse: %v\ngot: %s", err, got)
	}
	if f.Content != "line1\nline2" {
		t.Fatalf("decoded content mismatch: got %q", f.Content)
	}
}

// TestRepairJSONStringsBackslashBeforeNewline ensures the escape-state tracking
// is correct: a literal backslash inside a string must not swallow a following
// raw newline so that it goes unescaped.
func TestRepairJSONStringsBackslashBeforeNewline(t *testing.T) {
	// content is: <backslash><backslash> then a raw newline.
	broken := "{\"v\":\"\\\\\n\"}"
	repaired := repairJSONStrings(broken)

	var out struct {
		V string `json:"v"`
	}
	if err := json.Unmarshal([]byte(repaired), &out); err != nil {
		t.Fatalf("repaired JSON does not parse: %v\nrepaired: %q", err, repaired)
	}
	if out.V != "\\\n" {
		t.Fatalf("decoded value mismatch: got %q", out.V)
	}
}
