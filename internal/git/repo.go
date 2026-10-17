// Package git wraps go-git (pure Go, no system `git`) with the operations the
// git-* tools need. Every operation takes an *auth* resolved from config (the
// server selected by the remote URL's host).
package git

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	fdiff "github.com/go-git/go-git/v5/plumbing/format/diff"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

// Repo is a go-git repository handle.
type Repo = gogit.Repository

// Auth is the resolved credential for a remote (nil = anonymous).
type Auth struct {
	Username string
	Password string
}

// method converts Auth to a go-git transport method (nil when anonymous).
func (a *Auth) method() transport.AuthMethod {
	if a == nil || a.Password == "" {
		return nil
	}
	return &githttp.BasicAuth{Username: a.Username, Password: a.Password}
}

// Open opens an existing repository at dir.
func Open(dir string) (*gogit.Repository, error) {
	return gogit.PlainOpen(dir)
}

// IsRepo reports whether dir already holds a git repository.
func IsRepo(dir string) bool {
	_, err := gogit.PlainOpen(dir)
	return err == nil
}

// Clone clones remote into dir. depth=0 means full history; depth=1 a shallow
// snapshot. An existing non-empty dir is an error (callers use CloneOrOpen).
func Clone(ctx context.Context, remote, dir string, depth int, branch string, auth *Auth) error {
	opts := &gogit.CloneOptions{
		URL:   remote,
		Depth: depth,
		Auth:  auth.method(),
	}
	if branch != "" {
		opts.ReferenceName = plumbing.NewBranchReferenceName(branch)
		opts.SingleBranch = true
	}
	_, err := gogit.PlainCloneContext(ctx, dir, false, opts)
	return err
}

// RemoteURL returns the URL of the "origin" remote ("" when absent).
func RemoteURL(repo *gogit.Repository) string {
	rmt, err := repo.Remote("origin")
	if err != nil {
		return ""
	}
	urls := rmt.Config().URLs
	if len(urls) == 0 {
		return ""
	}
	return urls[0]
}

// Head returns the current branch name and commit hash.
func Head(repo *gogit.Repository) (branch, hash string, err error) {
	ref, err := repo.Head()
	if err != nil {
		return "", "", err
	}
	return ref.Name().Short(), ref.Hash().String(), nil
}

// StatusEntry is one changed path.
type StatusEntry struct {
	Path     string `json:"path"`
	Staging  string `json:"staging"` // "A" | "M" | "D" | "?" | " " ...
	Worktree string `json:"worktree"`
}

// Status reports the working-tree status.
func Status(repo *gogit.Repository) ([]StatusEntry, error) {
	wt, err := repo.Worktree()
	if err != nil {
		return nil, err
	}
	st, err := wt.Status()
	if err != nil {
		return nil, err
	}
	out := make([]StatusEntry, 0, len(st))
	for path, fs := range st {
		out = append(out, StatusEntry{Path: path, Staging: code(fs.Staging), Worktree: code(fs.Worktree)})
	}
	sortStatus(out)
	return out, nil
}

func code(s gogit.StatusCode) string {
	if s == gogit.Unmodified {
		return " "
	}
	return string(rune(s))
}

// LogEntry is one log entry.
type LogEntry struct {
	Hash    string    `json:"hash"`
	Author  string    `json:"author"`
	Email   string    `json:"email"`
	When    time.Time `json:"when"`
	Subject string    `json:"subject"`
}

// Log returns up to limit commits from HEAD (limit<=0 => 20).
func Log(repo *gogit.Repository, limit int) ([]LogEntry, error) {
	if limit <= 0 {
		limit = 20
	}
	iter, err := repo.Log(&gogit.LogOptions{})
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	out := []LogEntry{}
	err = iter.ForEach(func(c *object.Commit) error {
		if len(out) >= limit {
			return errStop
		}
		subject := c.Message
		if i := indexByte(subject, '\n'); i >= 0 {
			subject = subject[:i]
		}
		out = append(out, LogEntry{
			Hash:    c.Hash.String(),
			Author:  c.Author.Name,
			Email:   c.Author.Email,
			When:    c.Author.When,
			Subject: subject,
		})
		return nil
	})
	if err != nil && !errors.Is(err, errStop) {
		return nil, err
	}
	return out, nil
}

var errStop = errors.New("stop")

// sortStatus orders entries by path (stable output).
func sortStatus(in []StatusEntry) {
	sort.Slice(in, func(i, j int) bool { return in[i].Path < in[j].Path })
}

// indexByte is strings.IndexByte without importing strings for one call.
func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// pathOf returns the changed path of a tree change (new name wins).
func pathOf(ch *object.Change) string {
	if ch.To.Name != "" {
		return ch.To.Name
	}
	return ch.From.Name
}

// countPatch counts added/removed lines in a unified diff.
func countPatch(p *object.Patch) (adds, dels int) {
	for _, fp := range p.FilePatches() {
		for _, ch := range fp.Chunks() {
			n := strings.Count(ch.Content(), "\n")
			if n == 0 && ch.Content() != "" {
				n = 1
			}
			switch ch.Type() {
			case fdiff.Add:
				adds += n
			case fdiff.Delete:
				dels += n
			}
		}
	}
	return adds, dels
}

// BranchInfo is one branch (local or remote-tracking).
type BranchInfo struct {
	Name     string `json:"name"`
	Hash     string `json:"hash"`
	Current  bool   `json:"current"`
	IsRemote bool   `json:"isRemote"`
}

// Branches lists local branches (remote-tracking ones when remote=true).
func Branches(repo *gogit.Repository, remote bool) ([]BranchInfo, error) {
	head, _ := repo.Head()
	headName := ""
	if head != nil {
		headName = head.Name().Short()
	}
	iter, err := repo.Branches()
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	out := []BranchInfo{}
	err = iter.ForEach(func(ref *plumbing.Reference) error {
		name := ref.Name().Short()
		isRemote := ref.Name().IsRemote()
		if isRemote != remote {
			return nil
		}
		out = append(out, BranchInfo{Name: name, Hash: ref.Hash().String(), Current: !isRemote && name == headName, IsRemote: isRemote})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Checkout switches to (or creates) a branch.
func Checkout(repo *gogit.Repository, name string, create bool) error {
	wt, err := repo.Worktree()
	if err != nil {
		return err
	}
	return wt.Checkout(&gogit.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(name),
		Create: create,
	})
}

// Add stages paths (relative to the worktree). Empty = add everything.
func Add(repo *gogit.Repository, paths []string) error {
	wt, err := repo.Worktree()
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return wt.AddWithOptions(&gogit.AddOptions{All: true})
	}
	for _, p := range paths {
		if _, err := wt.Add(p); err != nil {
			return fmt.Errorf("git add %s: %w", p, err)
		}
	}
	return nil
}

// CommitOpts configures a commit.
type CommitOpts struct {
	Message string
	Name    string
	Email   string
	All     bool // stage tracked changes first
}

// Commit records a commit and returns its hash.
func Commit(repo *gogit.Repository, o CommitOpts) (string, error) {
	wt, err := repo.Worktree()
	if err != nil {
		return "", err
	}
	if o.All {
		if err := wt.AddWithOptions(&gogit.AddOptions{All: true}); err != nil {
			return "", err
		}
	}
	name := o.Name
	if name == "" {
		name = "easyagent"
	}
	email := o.Email
	if email == "" {
		email = "easyagent@localhost"
	}
	hash, err := wt.Commit(o.Message, &gogit.CommitOptions{
		Author: &object.Signature{Name: name, Email: email, When: time.Now()},
	})
	if err != nil {
		return "", err
	}
	return hash.String(), nil
}

// Pull fetches + merges the current branch from origin.
func Pull(ctx context.Context, repo *gogit.Repository, auth *Auth) error {
	wt, err := repo.Worktree()
	if err != nil {
		return err
	}
	err = wt.PullContext(ctx, &gogit.PullOptions{Auth: auth.method()})
	if errors.Is(err, gogit.NoErrAlreadyUpToDate) {
		return nil
	}
	return err
}

// Push pushes the current branch (or refspec) to origin. force uses
// force-with-lease semantics via +refspec.
func Push(ctx context.Context, repo *gogit.Repository, branch string, force bool, auth *Auth) error {
	refspec := config.RefSpec("refs/heads/" + branch + ":refs/heads/" + branch)
	if force {
		refspec = config.RefSpec("+refs/heads/" + branch + ":refs/heads/" + branch)
	}
	err := repo.PushContext(ctx, &gogit.PushOptions{
		Auth:     auth.method(),
		RefSpecs: []config.RefSpec{refspec},
	})
	if errors.Is(err, gogit.NoErrAlreadyUpToDate) {
		return nil
	}
	return err
}

// FileDiff is one changed file between two commits.
type FileDiff struct {
	Path      string `json:"path"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Patch     string `json:"patch,omitempty"`
}

// DiffCommits returns the file-level diff between two commit-ish revisions.
func DiffCommits(repo *gogit.Repository, from, to string) ([]FileDiff, error) {
	a, err := resolveCommit(repo, from)
	if err != nil {
		return nil, err
	}
	b, err := resolveCommit(repo, to)
	if err != nil {
		return nil, err
	}
	at, err := a.Tree()
	if err != nil {
		return nil, err
	}
	bt, err := b.Tree()
	if err != nil {
		return nil, err
	}
	changes, err := object.DiffTree(at, bt)
	if err != nil {
		return nil, err
	}
	out := make([]FileDiff, 0, len(changes))
	for _, ch := range changes {
		fd := FileDiff{Path: pathOf(ch)}
		if patch, err := ch.Patch(); err == nil {
			fd.Patch = patch.String()
			fd.Additions, fd.Deletions = countPatch(patch)
		}
		out = append(out, fd)
	}
	return out, nil
}

func resolveCommit(repo *gogit.Repository, rev string) (*object.Commit, error) {
	if rev == "" || rev == "HEAD" {
		ref, err := repo.Head()
		if err != nil {
			return nil, err
		}
		return repo.CommitObject(ref.Hash())
	}
	h, err := repo.ResolveRevision(plumbing.Revision(rev))
	if err != nil {
		return nil, fmt.Errorf("git: unknown revision %q: %w", rev, err)
	}
	return repo.CommitObject(*h)
}

// ShowFile returns a file's content at a revision.
func ShowFile(repo *gogit.Repository, rev, path string) (string, error) {
	c, err := resolveCommit(repo, rev)
	if err != nil {
		return "", err
	}
	f, err := c.File(path)
	if err != nil {
		return "", err
	}
	return f.Contents()
}
