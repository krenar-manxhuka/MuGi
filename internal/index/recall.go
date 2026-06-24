package index

import (
	"sort"
	"strings"
)

// ChangedFiles returns the repo-relative paths a unified diff modifies, taking
// the post-image ("+++ b/...") path so renames and edits are attributed to where
// the change lands. Pure additions from /dev/null are kept; deletions (post-image
// /dev/null) are dropped. This is how a gold patch tells us which files the fix
// touched — the ground truth for measuring retrieval.
func ChangedFiles(diff string) []string {
	set := map[string]bool{}
	for _, line := range strings.Split(diff, "\n") {
		if !strings.HasPrefix(line, "+++ ") {
			continue
		}
		p := strings.TrimSpace(line[len("+++ "):])
		// Strip a trailing tab-timestamp some diff tools append.
		if i := strings.IndexByte(p, '\t'); i >= 0 {
			p = p[:i]
		}
		if p == "/dev/null" || p == "" {
			continue
		}
		p = strings.TrimPrefix(p, "b/")
		set[p] = true
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// RecallAtK reports the fraction of changedFiles that appear among the retrieved
// chunks' file paths — i.e. did retrieval surface the files the fix actually
// needs? It needs no model generation, so it can be measured against gold patches
// for $0, and is the metric to tune chunking / k / fusion against before spending
// anything on the agent. Returns 0 when there are no changed files (nothing to
// recall).
func RecallAtK(retrieved []Chunk, changedFiles []string) float64 {
	if len(changedFiles) == 0 {
		return 0
	}
	got := make(map[string]bool, len(retrieved))
	for _, c := range retrieved {
		got[c.Path] = true
	}
	hit := 0
	for _, f := range changedFiles {
		if got[f] {
			hit++
		}
	}
	return float64(hit) / float64(len(changedFiles))
}
