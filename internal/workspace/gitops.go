package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

// Git operations on a workspace. All of them go through go-git, in-process.

// basicAuth is the token shape GitHub accepts for git-over-https.
func basicAuth(token string) *githttp.BasicAuth {
	// "x-access-token" is GitHub's documented username for token auth; the token is the
	// password. Using a real username also works, but this one works for every token type.
	return &githttp.BasicAuth{Username: "x-access-token", Password: token}
}

// open returns the go-git repository for a workspace.
func (w *Workspace) open() (*git.Repository, error) {
	return git.PlainOpen(w.Dir)
}

// FileStatus is one changed path.
type FileStatus struct {
	Path   string
	Status string // added | modified | deleted | renamed | untracked
	Staged bool
}

// Status reports what changed in the working tree, against HEAD.
//
// Includes untracked files: a file a session just wrote is a change, and hiding it until it is
// added would make `status` disagree with what the session did.
func (w *Workspace) Status() ([]FileStatus, error) {
	r, err := w.open()
	if err != nil {
		return nil, err
	}
	wt, err := r.Worktree()
	if err != nil {
		return nil, err
	}
	st, err := wt.Status()
	if err != nil {
		return nil, err
	}

	out := make([]FileStatus, 0, len(st))
	for path, s := range st {
		out = append(out, FileStatus{
			Path:   path,
			Status: statusName(s),
			Staged: s.Staging != git.Unmodified && s.Staging != git.Untracked,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func statusName(s *git.FileStatus) string {
	// The working-tree side wins when both are set: it is what the file looks like now.
	switch {
	case s.Worktree == git.Untracked:
		return "untracked"
	case s.Worktree == git.Added:
		return "added"
	case s.Worktree == git.Deleted:
		return "deleted"
	case s.Worktree == git.Modified, s.Staging == git.Modified:
		return "modified"
	case s.Staging == git.Deleted:
		return "deleted"
	case s.Staging == git.Added:
		return "added"
	case s.Staging == git.Renamed, s.Worktree == git.Renamed:
		return "renamed"
	}
	return "modified"
}

// ReadFile reads a path from the workspace, refusing anything outside it.
//
// The path check is not paranoia: a tool argument is caller-controlled, and "read ../../../etc"
// must not resolve.
func (w *Workspace) ReadFile(path string) ([]byte, error) {
	full, err := w.safePath(path)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(full)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%s does not exist in this workspace", path)
		}
		return nil, err
	}
	return b, nil
}

// WriteFile creates or replaces a file in the working tree.
func (w *Workspace) WriteFile(path string, content []byte) error {
	full, err := w.safePath(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, content, 0o644)
}

// RemoveFile deletes a file from the working tree.
func (w *Workspace) RemoveFile(path string) error {
	full, err := w.safePath(path)
	if err != nil {
		return err
	}
	if err := os.Remove(full); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s does not exist in this workspace", path)
		}
		return err
	}
	return nil
}

// ListFiles walks the working tree, skipping .git.
func (w *Workspace) ListFiles() ([]string, error) {
	var out []string
	err := filepath.Walk(w.Dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(w.Dir, p)
		if rerr != nil {
			return rerr
		}
		if rel == ".git" || strings.HasPrefix(rel, ".git"+string(filepath.Separator)) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(out)
	return out, err
}

// safePath resolves a caller-supplied path inside the workspace, or refuses it.
func (w *Workspace) safePath(p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", fmt.Errorf("path is required")
	}
	if strings.ContainsRune(p, 0) {
		return "", fmt.Errorf("path contains a NUL byte")
	}
	// Clean first so "a/../b" collapses before the escape check.
	clean := filepath.Clean(filepath.FromSlash(p))
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("path %q must be relative to the workspace", p)
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q may not leave the workspace", p)
	}
	if clean == ".git" || strings.HasPrefix(clean, ".git"+string(filepath.Separator)) {
		// Writing into .git would let a session rewrite the repository itself.
		return "", fmt.Errorf("path %q is inside .git, which is off limits", p)
	}
	return filepath.Join(w.Dir, clean), nil
}

// Add stages paths (or everything when none are given).
func (w *Workspace) Add(paths []string) error {
	r, err := w.open()
	if err != nil {
		return err
	}
	wt, err := r.Worktree()
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return wt.AddWithOptions(&git.AddOptions{All: true})
	}
	for _, p := range paths {
		if _, err := w.safePath(p); err != nil {
			return err
		}
		if _, err := wt.Add(p); err != nil {
			return fmt.Errorf("add %s: %w", p, err)
		}
	}
	return nil
}

// CommitOptions describes a commit.
type CommitOptions struct {
	Message string
	Author  string
	Email   string
}

// Commit records the staged changes.
func (w *Workspace) Commit(o CommitOptions) (string, error) {
	if strings.TrimSpace(o.Message) == "" {
		return "", fmt.Errorf("a commit message is required")
	}
	r, err := w.open()
	if err != nil {
		return "", err
	}
	wt, err := r.Worktree()
	if err != nil {
		return "", err
	}
	// Stage everything not yet staged: a session that wrote a file and committed expects the
	// file to be in the commit, not to be told it forgot `add`.
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		return "", err
	}
	sig := &object.Signature{Name: o.Author, Email: o.Email}
	if sig.Name == "" {
		sig.Name = "easyagent"
	}
	if sig.Email == "" {
		sig.Email = "easyagent@localhost"
	}
	// When MUST be set: go-git does not default it, so leaving it out produces commits dated
	// 1970-01-01. That is not cosmetic — the history a caller reads with git-ws-log would be wrong.
	sig.When = time.Now()
	h, err := wt.Commit(o.Message, &git.CommitOptions{Author: sig})
	if err != nil {
		return "", err
	}
	return h.String(), nil
}

// LogEntry is one commit in the workspace's history.
type LogEntry struct {
	SHA     string
	Message string
	Author  string
	Email   string
	When    string
}

// Log lists the last n commits reachable from HEAD.
//
// This is the capability the easyvcs-based design could not offer: real commit dates, real
// authors, real topology.
func (w *Workspace) Log(limit int) ([]LogEntry, error) {
	if limit <= 0 {
		limit = 20
	}
	r, err := w.open()
	if err != nil {
		return nil, err
	}
	head, err := r.Head()
	if err != nil {
		return nil, err
	}
	iter, err := r.Log(&git.LogOptions{From: head.Hash()})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	var out []LogEntry
	err = iter.ForEach(func(c *object.Commit) error {
		if len(out) >= limit {
			return nil
		}
		out = append(out, LogEntry{
			SHA:     c.Hash.String(),
			Message: strings.TrimSpace(c.Message),
			Author:  c.Author.Name,
			Email:   c.Author.Email,
			When:    c.Author.When.Format("2006-01-02 15:04:05"),
		})
		return nil
	})
	return out, err
}

// CurrentBranch is the checked-out branch, or "" when HEAD is detached.
func (w *Workspace) CurrentBranch() string {
	r, err := w.open()
	if err != nil {
		return ""
	}
	head, err := r.Head()
	if err != nil || !head.Name().IsBranch() {
		return ""
	}
	return head.Name().Short()
}

// CreateBranch switches the workspace to a new branch, created from HEAD.
func (w *Workspace) CreateBranch(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("branch name is required")
	}
	r, err := w.open()
	if err != nil {
		return err
	}
	wt, err := r.Worktree()
	if err != nil {
		return err
	}
	return wt.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(name),
		Create: true,
		Keep:   true,
	})
}

// PushResult reports what was pushed.
type PushResult struct {
	Branch string
	SHA    string
}

// Push publishes the current branch to the remote.
//
// Force is deliberately FALSE: a push that would overwrite somebody else's work must fail, not
// win. The branch name a caller uses is fresh, so a normal push suffices.
func (w *Workspace) Push(ctx context.Context, token string) (PushResult, error) {
	r, err := w.open()
	if err != nil {
		return PushResult{}, err
	}
	head, err := r.Head()
	if err != nil {
		return PushResult{}, err
	}
	if !head.Name().IsBranch() {
		return PushResult{}, fmt.Errorf("HEAD is detached; check out a branch before pushing")
	}
	branch := head.Name().Short()
	if branch == "" {
		return PushResult{}, fmt.Errorf("cannot push from an unnamed branch")
	}
	// ONLY the current branch. Pushing everything would also push the local main, which is
	// usually BEHIND the remote — and GitHub then reports the new branch as having no commits
	// of its own, refusing the pull request.
	pushOpts := &git.PushOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{config.RefSpec("refs/heads/" + branch + ":refs/heads/" + branch)},
	}
	if token != "" {
		pushOpts.Auth = basicAuth(token)
	}
	if err := r.PushContext(ctx, pushOpts); err != nil {
		// "already up to date" is a success from the caller's point of view.
		if errors.Is(err, git.NoErrAlreadyUpToDate) {
			return PushResult{Branch: branch, SHA: head.Hash().String()}, nil
		}
		return PushResult{}, fmt.Errorf("push %s: %w", branch, err)
	}
	return PushResult{Branch: branch, SHA: head.Hash().String()}, nil
}
