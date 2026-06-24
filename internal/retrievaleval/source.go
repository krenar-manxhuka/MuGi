package retrievaleval

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// RepoSource produces a local checkout of a repository at a specific commit. It
// is the seam between the pure recall measurement and *where* repositories come
// from: GitRepoSource fetches from a public host; tests use an in-process fake.
// The returned cleanup removes the checkout and must always be called.
type RepoSource interface {
	Checkout(ctx context.Context, repo, baseCommit string) (dir string, cleanup func(), err error)
}

// repoPattern and commitPattern constrain what may reach git. A repo is a single
// "owner/name" segment (no path traversal, no scheme, no flags); a commit is a
// short or full hex SHA. Validating here means neither argument can smuggle a
// leading dash that git would read as an option, or a URL pointing elsewhere.
var (
	repoPattern   = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	commitPattern = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)
)

// GitRepoSource materializes a repository at a base commit with a shallow,
// single-commit fetch over HTTPS — the cheapest checkout for read-only indexing.
// It never executes repository content; it only fetches and reads files. repo and
// baseCommit are validated against strict allowlists before reaching git, and git
// runs non-interactively, isolated from ambient config and credential helpers, so
// cloning a list of untrusted public repos stays a bounded, read-only operation.
//
// Because it fetches and stores arbitrary public repositories, run it inside the
// CI/sandbox boundary rather than on a development machine.
type GitRepoSource struct {
	BaseURL string        // host prefix; "" = "https://github.com/"
	WorkDir string        // parent dir for temp checkouts; "" = the OS temp dir
	Timeout time.Duration // per-git-command timeout; 0 = 5 minutes
}

// Checkout fetches baseCommit of repo into a fresh temp directory and returns it
// with a cleanup that deletes it.
func (g GitRepoSource) Checkout(ctx context.Context, repo, baseCommit string) (string, func(), error) {
	if !repoPattern.MatchString(repo) || strings.Contains(repo, "..") {
		// The pattern admits "." and ".." segments; reject any "name" that could
		// walk up a path, which matters if BaseURL is ever a local directory.
		return "", nil, fmt.Errorf("invalid repo %q", repo)
	}
	if !commitPattern.MatchString(baseCommit) {
		return "", nil, fmt.Errorf("invalid base commit %q", baseCommit)
	}
	base := g.BaseURL
	if base == "" {
		base = "https://github.com/"
	}
	timeout := g.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	dir, err := os.MkdirTemp(g.WorkDir, "mugi-recall-*")
	if err != nil {
		return "", nil, fmt.Errorf("temp dir: %w", err)
	}
	cleanup := func() { os.RemoveAll(dir) }

	url := base + repo + ".git"
	steps := [][]string{
		{"init", "--quiet"},
		{"remote", "add", "origin", url},
		// Fetch exactly the one commit, shallow, with no history.
		{"fetch", "--quiet", "--depth", "1", "origin", baseCommit},
		{"checkout", "--quiet", "--detach", "FETCH_HEAD"},
	}
	for _, args := range steps {
		if err := g.git(ctx, dir, timeout, args); err != nil {
			cleanup()
			return "", nil, err
		}
	}
	return dir, cleanup, nil
}

// git runs one git subcommand in dir under a timeout. The environment is pinned
// so git cannot prompt, read system/global config, or invoke a credential
// helper — the fetch is non-interactive and uses only what we pass it.
func (g GitRepoSource) git(ctx context.Context, dir string, timeout time.Duration, args []string) error {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",         // never prompt for credentials
		"GIT_CONFIG_NOSYSTEM=1",         // ignore /etc/gitconfig
		"GIT_CONFIG_GLOBAL="+os.DevNull, // ignore the user's ~/.gitconfig
		"GIT_ASKPASS=true",              // no interactive password helper
		"GCM_INTERACTIVE=never",         // no Git Credential Manager popups
	)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		if cctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("git %s: timed out after %s", args[0], timeout)
		}
		return fmt.Errorf("git %s: %w\n%s", args[0], err, strings.TrimSpace(buf.String()))
	}
	return nil
}
