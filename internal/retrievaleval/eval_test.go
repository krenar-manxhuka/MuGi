package retrievaleval

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mugi/internal/index"
)

// writeFixtureRepo lays down a tiny, language-agnostic repo on disk so the whole
// evaluation flow runs offline with no git and no network. calc.py carries the
// identifiers the query targets; the other files are distractors.
func writeFixtureRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"calc.py": "def divide_numbers(numerator, denominator):\n" +
			"    if denominator == 0:\n" +
			"        raise ZeroDivisionError(\"denominator is zero\")\n" +
			"    return numerator / denominator\n",
		"util/strings.py": "def shout(text):\n    return text.upper() + \"!\"\n\n\n" +
			"def whisper(text):\n    return text.lower()\n",
		"README.md": "# fixture\nA tiny repo for offline retrieval tests.\n",
	}
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// goldPatchFor is the minimal unified-diff header ChangedFiles needs to attribute
// a change to a file — the post-image path is the ground truth.
func goldPatchFor(path string) string {
	return "--- a/" + path + "\n+++ b/" + path + "\n@@ -1,1 +1,1 @@\n"
}

const divideQuery = "divide_numbers raises ZeroDivisionError when the denominator is zero"

func lexicalSpec(t *testing.T) []ModeSpec {
	t.Helper()
	b, err := ModeBuilder("lexical", nil)
	if err != nil {
		t.Fatalf("ModeBuilder(lexical): %v", err)
	}
	return []ModeSpec{{Label: "lexical", Build: b}}
}

func TestEvaluateTask_lexicalSurfacesChangedFile(t *testing.T) {
	dir := writeFixtureRepo(t)
	task := Task{
		ID:               "fix-divide",
		Repo:             "acme/calc",
		BaseCommit:       "deadbeef",
		ProblemStatement: divideQuery,
		GoldPatch:        goldPatchFor("calc.py"),
	}

	results, err := EvaluateTask(context.Background(), dir, task, Config{}, lexicalSpec(t), []int{1, 5})
	if err != nil {
		t.Fatalf("EvaluateTask: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2 (one per k)", len(results))
	}
	for _, r := range results {
		if r.Err != "" {
			t.Fatalf("k=%d unexpected error: %s", r.K, r.Err)
		}
		if len(r.ChangedFiles) != 1 || r.ChangedFiles[0] != "calc.py" {
			t.Fatalf("k=%d changed files = %v, want [calc.py]", r.K, r.ChangedFiles)
		}
		if r.RecallAtK != 1.0 || !r.FullHit {
			t.Errorf("k=%d recall=%.3f fullHit=%v, want 1.0/true (calc.py should rank top)",
				r.K, r.RecallAtK, r.FullHit)
		}
		if r.ChunksIndexed == 0 {
			t.Errorf("k=%d chunks indexed = 0", r.K)
		}
	}
}

func TestEvaluateTask_hybridWithMockEmbedder(t *testing.T) {
	dir := writeFixtureRepo(t)
	build, err := ModeBuilder("hybrid", index.MockEmbedder{})
	if err != nil {
		t.Fatalf("ModeBuilder(hybrid): %v", err)
	}
	task := Task{
		ID:               "fix-divide",
		ProblemStatement: divideQuery,
		GoldPatch:        goldPatchFor("calc.py"),
	}

	results, err := EvaluateTask(context.Background(), dir, task, Config{},
		[]ModeSpec{{Label: "hybrid", Build: build}}, []int{5})
	if err != nil {
		t.Fatalf("EvaluateTask hybrid: %v", err)
	}
	if len(results) != 1 || results[0].Err != "" {
		t.Fatalf("hybrid result = %+v", results)
	}
	// The mock embedder carries no meaning, but hybrid fuses in the lexical ranking,
	// which surfaces calc.py — so RRF still recalls the changed file at k=5.
	if results[0].RecallAtK != 1.0 {
		t.Errorf("hybrid recall@5 = %.3f, want 1.0 (lexical half should carry it)", results[0].RecallAtK)
	}
}

func TestEvaluateTask_missThenZeroRecall(t *testing.T) {
	dir := writeFixtureRepo(t)
	task := Task{
		ID:               "fix-missing",
		ProblemStatement: divideQuery,
		GoldPatch:        goldPatchFor("does/not/exist.py"),
	}
	results, err := EvaluateTask(context.Background(), dir, task, Config{}, lexicalSpec(t), []int{5})
	if err != nil {
		t.Fatalf("EvaluateTask: %v", err)
	}
	if results[0].RecallAtK != 0.0 || results[0].FullHit {
		t.Errorf("recall for an unreachable changed file = %.3f/%v, want 0.0/false",
			results[0].RecallAtK, results[0].FullHit)
	}
}

// fakeSource serves a fixed directory for any repo/commit, with no network.
type fakeSource struct {
	dir       string
	err       error
	checkouts int
	cleanups  int
}

func (f *fakeSource) Checkout(_ context.Context, _, _ string) (string, func(), error) {
	f.checkouts++
	if f.err != nil {
		return "", nil, f.err
	}
	return f.dir, func() { f.cleanups++ }, nil
}

func TestRun_aggregatesOverSliceAndReleasesCheckouts(t *testing.T) {
	dir := writeFixtureRepo(t)
	src := &fakeSource{dir: dir}
	tasks := []Task{
		{ID: "a", ProblemStatement: divideQuery, GoldPatch: goldPatchFor("calc.py")},
		{ID: "b", ProblemStatement: divideQuery, GoldPatch: goldPatchFor("calc.py")},
	}

	results := Run(context.Background(), src, tasks, Config{}, lexicalSpec(t), []int{5})

	if src.checkouts != 2 || src.cleanups != 2 {
		t.Fatalf("checkouts=%d cleanups=%d, want 2/2 (every checkout released)", src.checkouts, src.cleanups)
	}
	aggs := Summarize(results)
	if len(aggs) != 1 {
		t.Fatalf("got %d aggregate cells, want 1", len(aggs))
	}
	a := aggs[0]
	if a.Mode != "lexical" || a.K != 5 || a.N != 2 || a.Errors != 0 {
		t.Fatalf("aggregate = %+v", a)
	}
	if a.MeanRecall != 1.0 || a.FullHitRate != 1.0 {
		t.Errorf("meanRecall=%.3f fullHitRate=%.3f, want 1.0/1.0", a.MeanRecall, a.FullHitRate)
	}
}

func TestRun_checkoutFailureRecordedNotFatal(t *testing.T) {
	src := &fakeSource{err: errors.New("boom")}
	tasks := []Task{{ID: "a", GoldPatch: goldPatchFor("calc.py")}}

	results := Run(context.Background(), src, tasks, Config{}, lexicalSpec(t), []int{5})

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Err == "" {
		t.Errorf("expected a checkout error to be recorded on the result")
	}
	aggs := Summarize(results)
	if len(aggs) != 1 || aggs[0].Errors != 1 || aggs[0].N != 0 {
		t.Fatalf("aggregate = %+v, want errors=1 n=0", aggs)
	}
}

func TestRetrieveFrom_returnsRankedChunks(t *testing.T) {
	dir := writeFixtureRepo(t)
	b, err := ModeBuilder("lexical", nil)
	if err != nil {
		t.Fatal(err)
	}
	hits, err := RetrieveFrom(context.Background(), dir, divideQuery, Config{}, b, 3)
	if err != nil {
		t.Fatalf("RetrieveFrom: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected at least one chunk")
	}
	if hits[0].Path != "calc.py" {
		t.Errorf("top chunk = %s, want calc.py", hits[0].Path)
	}
}

func TestOracleChunks_onlyTheNamedFiles(t *testing.T) {
	dir := writeFixtureRepo(t)

	got, err := OracleChunks(dir, []string{"calc.py"}, Config{}, 5)
	if err != nil {
		t.Fatalf("OracleChunks: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected chunks from calc.py")
	}
	for _, c := range got {
		if c.Path != "calc.py" {
			t.Errorf("oracle returned a chunk from %s, want only calc.py", c.Path)
		}
	}

	// A file the gold patch names but that isn't present yields nothing.
	none, err := OracleChunks(dir, []string{"does/not/exist.py"}, Config{}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Errorf("expected no chunks for an absent file, got %d", len(none))
	}

	// k caps the number of chunks returned.
	capped, err := OracleChunks(dir, []string{"calc.py"}, Config{Window: 1, Overlap: 0}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(capped) != 1 {
		t.Errorf("k=1 should cap to 1 chunk, got %d", len(capped))
	}
}

func TestModeBuilder_validation(t *testing.T) {
	if _, err := ModeBuilder("lexical", nil); err != nil {
		t.Errorf("lexical needs no embedder: %v", err)
	}
	if _, err := ModeBuilder("semantic", nil); err == nil {
		t.Errorf("semantic without an embedder should error")
	}
	if _, err := ModeBuilder("hybrid", nil); err == nil {
		t.Errorf("hybrid without an embedder should error")
	}
	if _, err := ModeBuilder("nonsense", index.MockEmbedder{}); err == nil {
		t.Errorf("unknown mode should error")
	}
}

func TestSortedUnique(t *testing.T) {
	got := sortedUnique([]int{20, 5, 5, 0, -3, 10, 20})
	want := []int{5, 10, 20}
	if len(got) != len(want) {
		t.Fatalf("sortedUnique = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sortedUnique = %v, want %v", got, want)
		}
	}
}

func TestCapRunes(t *testing.T) {
	if got := capRunes("hello", 0); got != "hello" {
		t.Errorf("no cap should pass through, got %q", got)
	}
	if got := capRunes("hello", 3); got != "hel" {
		t.Errorf("capRunes(hello,3) = %q, want hel", got)
	}
	if got := capRunes("hi", 5); got != "hi" {
		t.Errorf("short string should pass through, got %q", got)
	}
}
