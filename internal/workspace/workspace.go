// Package workspace holds the per-session git working directories.
//
// WHY A WORKSPACE AT ALL, and why it is a real directory:
//
// The extension used to keep edits in an easyvcs revision database, read through its own tree
// model. That worked, but it stored a SIMPLIFIED history — no commit timestamps, flattened
// merge topology, tag messages dropped — so anything a caller might ask about the repository's
// past could be answered wrongly. A real git working directory has the actual history, because
// it IS the actual repository.
//
// WHY NO SHELL. Every git operation here goes through go-git, in-process. There is deliberately
// no `exec.Command("git", ...)` anywhere: a session may edit files and commit, and that is all.
// Anything that needs to RUN something runs in GitHub Actions, chosen by the repository's own
// workflow, where the repository's own CI rules apply. An agent that could execute arbitrary
// commands here would make the pull request a formality.
package workspace

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
)

// ErrNoWorkspace means no workspace exists for that id.
var ErrNoWorkspace = errors.New("no workspace with that id")

// ErrNotOwned means the workspace belongs to another session.
//
// Checked rather than ignored: an id is a capability, and a session that guessed another's id
// must not be able to read or push its work.
var ErrNotOwned = errors.New("that workspace belongs to a different session")

// Workspace is one session's checkout.
type Workspace struct {
	// ID is the random handle tools take as an argument. Never derived from the repository, so
	// it cannot be guessed from it.
	ID string
	// Repo is owner/name.
	Repo string
	// Dir is the working directory on disk.
	Dir string
	// Tenant and Session record who created it.
	Tenant  string
	Session string
	// CreatedAt is used to expire abandoned workspaces.
	CreatedAt time.Time
}

// Manager owns the workspaces on this process's disk.
//
// STATE LIVES ONLY HERE, in memory, on purpose. A workspace is a scratchpad for one session's
// work; persisting the registry would mean a pod restart resurrected handles to directories that
// no longer exist. The DIRECTORY may survive (it is on disk), but a caller must clone again to
// get a handle to it — which is the honest behaviour.
type Manager struct {
	mu    sync.Mutex
	root  string
	areas map[string]*Workspace
	// idleTTL expires a workspace nobody has touched. Without it a long-lived pod accumulates
	// checkouts until the disk fills.
	idleTTL time.Duration
	lastUse map[string]time.Time
}

// NewManager keeps workspaces under root.
func NewManager(root string, idleTTL time.Duration) (*Manager, error) {
	// Before any clone or push, so a proxied cluster can reach GitHub at all.
	installProxyTransport()
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("workspace root %s: %w", root, err)
	}
	if idleTTL <= 0 {
		idleTTL = 2 * time.Hour
	}
	return &Manager{root: root, areas: map[string]*Workspace{}, idleTTL: idleTTL, lastUse: map[string]time.Time{}}, nil
}

// CloneOptions describes the checkout to create.
type CloneOptions struct {
	Tenant  string
	Session string
	Repo    string // owner/name
	URL     string // https://github.com/owner/name
	// Ref is a branch, tag or commit. Empty = the remote's default branch.
	Ref   string
	Token string
	// Depth is passed to go-git. 0 = full history, which is the default because a caller may
	// legitimately ask about an old commit or a tag.
	Depth int
}

// IDPattern is what a workspace id looks like. Validated on every use so a malformed id is
// rejected before it reaches the filesystem.
var IDPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

// Clone creates a workspace and returns it.
func (m *Manager) Clone(o CloneOptions) (*Workspace, error) {
	return m.CloneCtx(context.Background(), o)
}

// CloneCtx is Clone with a context, so the clone reports its own progress.
//
// go-git's Progress writer receives the text a git clone prints, and newGitProgress turns its
// percentages into updates. Passing nil keeps go-git silent, which is what a caller with no
// listener wants.
func (m *Manager) CloneCtx(ctx context.Context, o CloneOptions) (*Workspace, error) {
	if o.Repo == "" || o.URL == "" {
		return nil, fmt.Errorf("repo and url are required")
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(m.root, id)

	co := &git.CloneOptions{URL: o.URL, Depth: o.Depth}
	if _, listening := ctx.Value(progressKey{}).(PhaseReporter); listening {
		co.Progress = newGitProgress(ctx)
	}
	if o.Ref != "" {
		// Try as a branch, then a tag: a caller says "v1.2.3" without saying which it is.
		co.ReferenceName = plumbing.NewBranchReferenceName(o.Ref)
		co.SingleBranch = true
	}
	if o.Token != "" {
		co.Auth = basicAuth(o.Token)
	}

	repo, err := git.PlainClone(dir, false, co)
	if err != nil {
		// A tag is not a branch. Retry once with the tag reference before reporting failure,
		// so "clone v1.2.3" works whether or not a branch of that name exists.
		if o.Ref != "" && errors.Is(err, plumbing.ErrReferenceNotFound) {
			_ = os.RemoveAll(dir)
			co.ReferenceName = plumbing.NewTagReferenceName(o.Ref)
			repo, err = git.PlainClone(dir, false, co)
		}
		if err != nil {
			_ = os.RemoveAll(dir)
			return nil, fmt.Errorf("clone %s: %w", o.Repo, wrapCloneErr(err))
		}
	}
	// A single-branch clone is fine, but it must not be SHALLOW unless asked: a shallow
	// checkout cannot answer questions about older commits, and this package exists so that
	// history questions have real answers.
	_ = repo

	w := &Workspace{
		ID: id, Repo: o.Repo, Dir: dir,
		Tenant: o.Tenant, Session: o.Session, CreatedAt: time.Now(),
	}
	m.mu.Lock()
	m.areas[id] = w
	m.lastUse[id] = time.Now()
	m.mu.Unlock()
	return w, nil
}

// Get returns a workspace, verifying the caller owns it.
func (m *Manager) Get(id, tenant, session string) (*Workspace, error) {
	if !IDPattern.MatchString(id) {
		return nil, ErrNoWorkspace
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.areas[id]
	if !ok {
		return nil, ErrNoWorkspace
	}
	if w.Tenant != tenant || w.Session != session {
		return nil, ErrNotOwned
	}
	m.lastUse[id] = time.Now()
	return w, nil
}

// List returns the caller's workspaces.
func (m *Manager) List(tenant, session string) []*Workspace {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Workspace, 0, len(m.areas))
	for _, w := range m.areas {
		if w.Tenant == tenant && w.Session == session {
			out = append(out, w)
		}
	}
	return out
}

// Close removes a workspace from disk and forgets it.
func (m *Manager) Close(id, tenant, session string) error {
	w, err := m.Get(id, tenant, session)
	if err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.areas, w.ID)
	delete(m.lastUse, w.ID)
	m.mu.Unlock()
	return os.RemoveAll(w.Dir)
}

// SweepIdle removes workspaces nobody has touched within the TTL.
//
// Called on a timer by the process. A workspace that vanishes this way is not an error a caller
// can see: it finds out when its next call says "no workspace with that id", which is exactly
// what happened.
func (m *Manager) SweepIdle(now time.Time) (removed int) {
	m.mu.Lock()
	var stale []*Workspace
	for id, w := range m.areas {
		if now.Sub(m.lastUse[id]) > m.idleTTL {
			stale = append(stale, w)
		}
	}
	for _, w := range stale {
		delete(m.areas, w.ID)
		delete(m.lastUse, w.ID)
	}
	m.mu.Unlock()

	for _, w := range stale {
		_ = os.RemoveAll(w.Dir)
		removed++
	}
	return removed
}

func newID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// wrapCloneErr turns the two failures a caller can act on into sentences.
func wrapCloneErr(err error) error {
	switch {
	case errors.Is(err, transport.ErrAuthenticationRequired):
		return fmt.Errorf("%w — this repository is private or does not exist; a token with access is needed", err)
	case errors.Is(err, transport.ErrRepositoryNotFound):
		return fmt.Errorf("%w — check the owner/name", err)
	}
	return err
}
