package predict

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mugi/internal/index"
	"mugi/internal/llm"
)

const cannedDiff = `diff --git a/calc.py b/calc.py
--- a/calc.py
+++ b/calc.py
@@ -1,2 +1,2 @@
 def divide(n, d):
-    return n / d
+    return n / d if d else 0`

func calcChunks() []index.Chunk {
	return []index.Chunk{
		{Path: "calc.py", Start: 1, End: 2, Content: "def divide(n, d):\n    return n / d"},
		{Path: "util.py", Start: 1, End: 1, Content: "def noop():\n    pass"},
	}
}

// mockReturning builds a provider that replies with body whenever the system
// prompt names a unified diff (which the swebench_diff template does).
func mockReturning(body string) llm.Provider {
	return &llm.MockProvider{CustomResponses: map[string]string{"unified diff": body}}
}

// errProvider always fails, standing in for a network/auth error.
type errProvider struct{}

func (errProvider) Name() string { return "err" }
func (errProvider) Generate(context.Context, llm.Request) (llm.Response, error) {
	return llm.Response{}, fmt.Errorf("upstream exploded")
}

func TestPredict_fencedDiffBecomesPrediction(t *testing.T) {
	g := Generator{Provider: mockReturning("```diff\n" + cannedDiff + "\n```")}
	res := g.Predict(context.Background(), Task{ID: "calc-1", Repo: "acme/calc",
		ProblemStatement: "divide crashes on zero"}, calcChunks())

	if res.Err != "" {
		t.Fatalf("unexpected error: %s", res.Err)
	}
	if res.Prediction.InstanceID != "calc-1" {
		t.Errorf("instance id = %q", res.Prediction.InstanceID)
	}
	if res.Prediction.Model != "mock" {
		t.Errorf("model = %q, want mock (provider name)", res.Prediction.Model)
	}
	if !strings.HasPrefix(res.Prediction.Patch, "diff --git a/calc.py") {
		t.Errorf("patch was not unwrapped from the fence:\n%s", res.Prediction.Patch)
	}
	if strings.Contains(res.Prediction.Patch, "```") {
		t.Errorf("patch still contains a code fence")
	}
	if len(res.ChangedFiles) != 1 || res.ChangedFiles[0] != "calc.py" {
		t.Errorf("changed files = %v, want [calc.py]", res.ChangedFiles)
	}
	if !res.PlausibleHit {
		t.Errorf("diff touches calc.py, which was retrieved — PlausibleHit should be true")
	}
}

func TestPredict_rawDiffAlsoWorks(t *testing.T) {
	g := Generator{Provider: mockReturning("Sure, here is the fix:\n" + cannedDiff)}
	res := g.Predict(context.Background(), Task{ID: "calc-1"}, calcChunks())
	if res.Err != "" {
		t.Fatalf("unexpected error: %s", res.Err)
	}
	if !strings.HasPrefix(res.Prediction.Patch, "diff --git a/calc.py") {
		t.Errorf("leading prose was not stripped:\n%q", res.Prediction.Patch)
	}
}

func TestPredict_modelNameOverride(t *testing.T) {
	g := Generator{Provider: mockReturning(cannedDiff), Config: Config{ModelName: "mugi/claude"}}
	res := g.Predict(context.Background(), Task{ID: "x"}, calcChunks())
	if res.Prediction.Model != "mugi/claude" {
		t.Errorf("model = %q, want the override", res.Prediction.Model)
	}
}

func TestPredict_proseReplyIsRecordedAsMiss(t *testing.T) {
	g := Generator{Provider: mockReturning("I could not find the bug.")}
	res := g.Predict(context.Background(), Task{ID: "calc-1"}, calcChunks())
	if res.Err == "" {
		t.Fatalf("a non-diff reply should be a recorded miss")
	}
	if res.Prediction.Patch != "" {
		t.Errorf("missed prediction should have an empty patch, got %q", res.Prediction.Patch)
	}
	if res.Prediction.InstanceID != "calc-1" {
		t.Errorf("the row must still carry the instance id so it counts as unresolved")
	}
}

func TestPredict_providerErrorIsRecorded(t *testing.T) {
	g := Generator{Provider: errProvider{}}
	res := g.Predict(context.Background(), Task{ID: "calc-1"}, calcChunks())
	if res.Err == "" || res.Prediction.Patch != "" {
		t.Fatalf("provider error should yield an empty patch and a recorded error: %+v", res)
	}
}

func TestPredict_emptyContextStillPrompts(t *testing.T) {
	g := Generator{Provider: mockReturning(cannedDiff)}
	res := g.Predict(context.Background(), Task{ID: "x"}, nil)
	if res.Err != "" {
		t.Fatalf("no-context generation should still run: %s", res.Err)
	}
	// Nothing retrieved, so a real hit can't be confirmed.
	if res.PlausibleHit {
		t.Errorf("PlausibleHit should be false when nothing was retrieved")
	}
}

func TestUserMessage_boundsAndLabels(t *testing.T) {
	chunks := []index.Chunk{
		{Path: "a.py", Start: 1, End: 3, Content: strings.Repeat("x", 50)},
		{Path: "b.py", Start: 4, End: 6, Content: "second"},
		{Path: "c.py", Start: 7, End: 9, Content: "third"},
	}
	msg := userMessage(Task{ProblemStatement: "  boom  "}, chunks,
		Config{MaxContextChunks: 2, MaxChunkRunes: 10}.withDefaults())

	if !strings.Contains(msg, "boom") {
		t.Error("problem statement missing from prompt")
	}
	if !strings.Contains(msg, "File: a.py  (lines 1-3)") {
		t.Error("first excerpt label missing")
	}
	if strings.Contains(msg, "c.py") {
		t.Error("MaxContextChunks=2 should have dropped the third excerpt")
	}
	if !strings.Contains(msg, "truncated") {
		t.Error("MaxChunkRunes should have truncated the long excerpt")
	}
}

func TestWritePredictions_jsonlSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "predictions.jsonl")
	preds := []Prediction{
		{InstanceID: "a", Model: "m", Patch: "diff --git a/x b/x\n"},
		{InstanceID: "b", Model: "m", Patch: ""}, // unresolved row still written
	}
	if err := WritePredictions(path, preds); err != nil {
		t.Fatalf("WritePredictions: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("line 1 is not valid JSON: %v", err)
	}
	for _, key := range []string{"instance_id", "model_name_or_path", "model_patch"} {
		if _, ok := first[key]; !ok {
			t.Errorf("prediction row is missing %q (the harness needs it)", key)
		}
	}
}
