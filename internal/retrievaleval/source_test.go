package retrievaleval

import (
	"context"
	"testing"
)

// TestGitRepoSource_rejectsUnsafeInput proves the boundary validation fires
// before git is ever invoked: malformed repo names and non-hex commits are
// refused, so no crafted argument can reach the command. These cases touch no
// network — a valid repo/commit would, so they are exercised only on CI.
func TestGitRepoSource_rejectsUnsafeInput(t *testing.T) {
	src := GitRepoSource{}
	cases := []struct {
		name   string
		repo   string
		commit string
	}{
		{"space in repo", "acme calc", "deadbeef"},
		{"path traversal", "../../etc", "deadbeef"},
		{"dotdot prefix", "../x", "deadbeef"},
		{"dotdot name", "owner/..", "deadbeef"},
		{"leading dash repo", "-flag/x", "deadbeef"},
		{"too many segments", "a/b/c", "deadbeef"},
		{"non-hex commit", "acme/calc", "not-a-sha"},
		{"leading dash commit", "acme/calc", "-rf"},
		{"empty commit", "acme/calc", ""},
		{"too-short commit", "acme/calc", "abc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir, cleanup, err := src.Checkout(context.Background(), tc.repo, tc.commit)
			if err == nil {
				if cleanup != nil {
					cleanup()
				}
				t.Fatalf("Checkout(%q,%q) = %q, want validation error", tc.repo, tc.commit, dir)
			}
		})
	}
}

func TestGitRepoSource_acceptsWellFormedInput(t *testing.T) {
	// Well-formed inputs must pass validation (they would then hit the network, so
	// we only assert the patterns accept them, not the fetch).
	if !repoPattern.MatchString("psf/requests") {
		t.Error("psf/requests should be a valid repo")
	}
	if !repoPattern.MatchString("pallets/flask") {
		t.Error("pallets/flask should be a valid repo")
	}
	if !commitPattern.MatchString("0a1b2c3") {
		t.Error("a 7-char hex prefix should be a valid commit")
	}
	if !commitPattern.MatchString("0123456789abcdef0123456789abcdef01234567") {
		t.Error("a 40-char SHA should be a valid commit")
	}
}
