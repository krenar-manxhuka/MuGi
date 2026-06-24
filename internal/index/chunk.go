// Package index builds a retrieval index over a repository so an agent can be
// shown only the code relevant to a task, rather than a whole repo that cannot
// fit in a context window.
//
// The package is a pure core, testable offline with no network: a Chunker turns
// a checked-out repo into retrievable units, an Index ranks them against a query
// (lexical BM25 today; an Embedder seam is in place for semantic/hybrid retrieval
// next), and a recall@k helper measures retrieval quality against a gold patch
// for $0 — no model generation required.
//
// Indexing is read-only text processing: it never executes the repository. File
// walking skips symlinks (no traversal escape), binaries, oversized files, and
// common dependency directories, and is bounded by an explicit chunk cap.
package index

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Chunk is one retrievable unit of a repository: a contiguous line range of a
// single text file.
type Chunk struct {
	Path    string // repo-relative, forward-slash separated
	Start   int    // 1-based first line (inclusive)
	End     int    // 1-based last line (inclusive)
	Content string
}

// Chunker turns a checked-out repository rooted at dir into retrievable chunks.
type Chunker interface {
	Chunk(root string) ([]Chunk, error)
}

// WindowChunker splits each text file into overlapping fixed-size line windows.
// It is deliberately language-agnostic (SWE-bench repos are Python; MuGi is Go),
// so it works across languages; AST-aware chunking is a later refinement.
type WindowChunker struct {
	WindowLines  int   // lines per chunk (default 50)
	OverlapLines int   // overlap between adjacent chunks (default 10)
	MaxFileBytes int64 // skip files larger than this (default 1 MiB)
	MaxChunks    int   // hard cap across the whole repo (default 50000)
}

// errChunkCap is an internal sentinel used to stop walking once MaxChunks is hit;
// it is swallowed so the caller gets the chunks gathered so far, not an error.
var errChunkCap = errors.New("index: chunk cap reached")

// skipDirs are dependency / VCS / cache directories never worth indexing.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true,
	".venv": true, "venv": true, "__pycache__": true,
	".mypy_cache": true, ".pytest_cache": true, ".tox": true,
	".idea": true, ".vscode": true,
}

func (c WindowChunker) withDefaults() WindowChunker {
	if c.WindowLines <= 0 {
		c.WindowLines = 50
	}
	if c.OverlapLines < 0 || c.OverlapLines >= c.WindowLines {
		c.OverlapLines = c.WindowLines / 5
	}
	if c.MaxFileBytes <= 0 {
		c.MaxFileBytes = 1 << 20
	}
	if c.MaxChunks <= 0 {
		c.MaxChunks = 50000
	}
	return c
}

// Chunk walks root and returns chunks for every indexable text file. Unreadable
// files are skipped rather than failing the whole index.
func (c WindowChunker) Chunk(root string) ([]Chunk, error) {
	c = c.withDefaults()
	step := c.WindowLines - c.OverlapLines
	if step < 1 {
		step = c.WindowLines
	}

	var chunks []Chunk
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Unreadable directory: skip it, don't abort the whole walk.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if path != root && skipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		// Never follow symlinks — they can point outside root.
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > c.MaxFileBytes {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || isBinary(data) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		lines := strings.Split(string(data), "\n")
		n := len(lines)
		for start := 0; start < n; start += step {
			end := start + c.WindowLines
			if end > n {
				end = n
			}
			content := strings.Join(lines[start:end], "\n")
			if strings.TrimSpace(content) != "" {
				chunks = append(chunks, Chunk{Path: rel, Start: start + 1, End: end, Content: content})
				if len(chunks) >= c.MaxChunks {
					return errChunkCap
				}
			}
			if end == n {
				break
			}
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, errChunkCap) {
		return chunks, walkErr
	}
	return chunks, nil
}

// isBinary reports whether data looks like a binary file (contains a NUL byte in
// the first 8 KiB). Binary files are not useful retrieval text.
func isBinary(data []byte) bool {
	n := len(data)
	if n > 8192 {
		n = 8192
	}
	for i := 0; i < n; i++ {
		if data[i] == 0 {
			return true
		}
	}
	return false
}
