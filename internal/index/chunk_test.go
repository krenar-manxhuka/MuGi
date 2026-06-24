package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowChunker_ChunksAndSkips(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Indexable: a multi-window source file and a tiny one.
	var sb strings.Builder
	for i := 0; i < 60; i++ {
		sb.WriteString("ln\n")
	}
	write("a.go", sb.String())
	write("one.txt", "alpha\nbeta\ngamma\n")

	// Must be skipped:
	write("bin.dat", "ok\x00more")                        // binary (NUL byte)
	write("big.txt", strings.Repeat("x", 1500))           // over MaxFileBytes
	write(".git/config", "[core]\n")                      // VCS dir
	write("node_modules/dep/x.js", "module.exports={}\n") // dependency dir

	chunks, err := WindowChunker{WindowLines: 20, OverlapLines: 5, MaxFileBytes: 1000}.Chunk(root)
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}

	paths := map[string]int{}
	for _, c := range chunks {
		paths[c.Path]++
		if c.Start < 1 || c.End < c.Start {
			t.Fatalf("bad line range for %s: %d-%d", c.Path, c.Start, c.End)
		}
		if strings.ContainsAny(c.Path, "\\") {
			t.Fatalf("path not forward-slash normalized: %q", c.Path)
		}
	}

	if paths["a.go"] < 2 {
		t.Errorf("expected a.go to produce multiple chunks, got %d", paths["a.go"])
	}
	if paths["one.txt"] != 1 {
		t.Errorf("expected one.txt to produce 1 chunk, got %d", paths["one.txt"])
	}
	for _, skipped := range []string{"bin.dat", "big.txt", ".git/config", "node_modules/dep/x.js"} {
		if paths[skipped] != 0 {
			t.Errorf("expected %q to be skipped, got %d chunks", skipped, paths[skipped])
		}
	}
}

func TestWindowChunker_SkipsSymlinks(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real.txt")
	if err := os.WriteFile(real, []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(root, "link.txt")); err != nil {
		t.Skip("symlinks unavailable on this host: " + err.Error())
	}

	chunks, err := (WindowChunker{}).Chunk(root)
	if err != nil {
		t.Fatal(err)
	}
	var sawReal bool
	for _, c := range chunks {
		if c.Path == "link.txt" {
			t.Fatal("symlink must not be indexed (traversal-escape guard)")
		}
		if c.Path == "real.txt" {
			sawReal = true
		}
	}
	if !sawReal {
		t.Fatal("the real file should still be indexed")
	}
}

func TestWindowChunker_ChunkCap(t *testing.T) {
	root := t.TempDir()
	var sb strings.Builder
	for i := 0; i < 500; i++ {
		sb.WriteString("x\n")
	}
	if err := os.WriteFile(filepath.Join(root, "big.go"), []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	chunks, err := WindowChunker{WindowLines: 5, OverlapLines: 0, MaxChunks: 3, MaxFileBytes: 1 << 20}.Chunk(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 3 {
		t.Fatalf("expected chunk cap to stop at 3, got %d", len(chunks))
	}
}

func TestIsBinary(t *testing.T) {
	if !isBinary([]byte("abc\x00def")) {
		t.Error("NUL byte should be detected as binary")
	}
	if isBinary([]byte("plain text\nwith lines")) {
		t.Error("plain text should not be binary")
	}
}
