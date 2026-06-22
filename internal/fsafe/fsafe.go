// Package fsafe provides path-containment helpers for safely writing files
// whose names come from untrusted sources (e.g. LLM-generated artifacts).
//
// The threat: a model can emit a file path like "../../etc/cron.d/x" or an
// absolute path. filepath.Join happily resolves "../" outside the base, so
// joining a model-controlled path to an output directory is a path-traversal
// vulnerability. These helpers make that class of bug structurally impossible
// at the write boundary.
package fsafe

import (
	"fmt"
	"path/filepath"
	"strings"
)

// SafeRelPath reports an error if p is not a path that stays within a base
// directory: empty, absolute, rooted, or containing a "../" component that
// climbs out. It is a pure check and never touches the filesystem.
func SafeRelPath(p string) error {
	if strings.TrimSpace(p) == "" {
		return fmt.Errorf("fsafe: empty path")
	}
	if filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) {
		return fmt.Errorf("fsafe: rooted/absolute path not allowed: %q", p)
	}
	// Reject Windows volume/drive paths on EVERY OS, not just Windows.
	// filepath.IsAbs and filepath.VolumeName only recognise a drive letter
	// ("C:\x", "C:/x", "C:x") or UNC volume as absolute *on Windows*; on a Linux
	// host such a path is just a relative name with a colon and would otherwise
	// slip through (a cross-platform CI run caught exactly this). A contained
	// relative path never carries a drive letter.
	if filepath.VolumeName(p) != "" || hasDriveLetter(p) {
		return fmt.Errorf("fsafe: volume/drive-qualified path not allowed: %q", p)
	}
	clean := filepath.Clean(filepath.FromSlash(p))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("fsafe: path escapes base: %q", p)
	}
	return nil
}

// hasDriveLetter reports whether p starts with a Windows drive-letter prefix
// (an ASCII letter followed by ':'), e.g. "C:", "c:". This is recognised on all
// operating systems so a model-emitted artifact path with a drive letter is
// rejected regardless of the host the harness runs on.
func hasDriveLetter(p string) bool {
	return len(p) >= 2 && p[1] == ':' &&
		((p[0] >= 'a' && p[0] <= 'z') || (p[0] >= 'A' && p[0] <= 'Z'))
}

// SafeJoin joins base and a relative path p, guaranteeing the result stays
// inside base. It returns an error for any path that would escape, so callers
// can use the returned path without further checks.
func SafeJoin(base, p string) (string, error) {
	if err := SafeRelPath(p); err != nil {
		return "", err
	}
	joined := filepath.Join(base, filepath.FromSlash(p))

	// As an extra safeguard: confirm containment on the resolved paths, independent
	// of the lexical pre-check above.
	absBase, err := filepath.Abs(base)
	if err != nil {
		return "", fmt.Errorf("fsafe: resolve base: %w", err)
	}
	absJoined, err := filepath.Abs(joined)
	if err != nil {
		return "", fmt.Errorf("fsafe: resolve path: %w", err)
	}
	rel, err := filepath.Rel(absBase, absJoined)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("fsafe: path %q escapes base %q", p, base)
	}
	return joined, nil
}
