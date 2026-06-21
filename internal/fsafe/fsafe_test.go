package fsafe

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeRelPathRejectsTraversalAndAbsolute(t *testing.T) {
	bad := []string{
		"",
		"   ",
		"../escape.go",
		"../../etc/passwd",
		"a/../../b",
		"/etc/passwd",
		`\windows\system32`,
		filepath.Join("C:", "abs", "path"), // absolute on Windows
	}
	for _, p := range bad {
		if err := SafeRelPath(p); err == nil {
			t.Errorf("expected SafeRelPath(%q) to fail", p)
		}
	}
}

func TestSafeRelPathAllowsNormalPaths(t *testing.T) {
	good := []string{"main.go", "pkg/util.go", "a/b/c/file.txt", "./local.go"}
	for _, p := range good {
		if err := SafeRelPath(p); err != nil {
			t.Errorf("expected SafeRelPath(%q) to pass, got %v", p, err)
		}
	}
}

func TestSafeJoinContains(t *testing.T) {
	base := t.TempDir()

	got, err := SafeJoin(base, "sub/dir/file.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(got, base) {
		t.Fatalf("joined path %q is not under base %q", got, base)
	}

	if _, err := SafeJoin(base, "../../evil.go"); err == nil {
		t.Fatal("expected traversal to be rejected by SafeJoin")
	}
}
