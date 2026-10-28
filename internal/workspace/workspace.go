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
	"sort"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
)

// ErrNoWorkspace means no workspace exists for that id.
//
// The MESSAGE is written for whoever reads it — a person looking at a panel that failed to load, or
// a model deciding what to do next. It used to be "no workspace with that id", which is true and
// useless: the common cause is not a typo but a RESTART. The registry is in memory on purpose (a
// checkout is a scratchpad, and a stale handle to a directory that moved is worse than none), so
// every pod restart drops every handle while the ids in old chat messages still look valid.
//
// Saying that turns a dead end into one next step.
var ErrNoWorkspace = errors.New(
	"no such workspace — it may have expired. Workspaces live in memory, so a service restart " +
		"clears them; run git-ws-clone again to get a new one")

// ErrBadWorkspaceID means the id is not even shaped like one.
//
// Kept SEPARATE from ErrNoWorkspace: an expired handle and a malformed string need different
// answers ("clone again" vs "that is not an id"), and folding them together sends the reader to redo
// work that was never the problem.
var ErrBadWorkspaceID = errors.New("that is not a workspace id")

// ErrNotOwned means the workspace belongs to ANOTHER TENANT.
//
// The unit of ownership is the TENANT, not the session. A checkout is one of the tenant's own
// resources — it is their repository, on their disk, discovered through their token — and the same
// person browsing from a different conversation, or from the extension's own tab, is still the same
// person. Session-level ownership made that impossible: a list could show a checkout and the very
// next click would refuse to open it, which is not a security boundary but a broken link.
//
// What still holds, and must: an id is a capability, and ANOTHER TENANT that guesses one must not be
// able to read or push the work. That is what this error now reports, and `Get` enforces it before
// anything else.
var ErrNotOwned = errors.New("that workspace belongs to a different tenant")

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

// Get returns a workspace, verifying it belongs to the caller's TENANT.
//
// The session is still a parameter and still recorded on the workspace — it is what tells a reader
// which conversation a checkout came from — but it is NOT an access boundary. Ownership is the
// tenant's: the checkout is their repository on their disk, and moving between conversations (or
// into the extension's own tab) does not change who owns it.
//
// `_` rather than a rename so callers keep passing the scope they have; an access check that ignores
// a value should say so at the signature, not in a comment.
func (m *Manager) Get(id, tenant, _ string) (*Workspace, error) {
	if !IDPattern.MatchString(id) {
		return nil, ErrBadWorkspaceID
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.areas[id]
	if !ok {
		return nil, ErrNoWorkspace
	}
	// The ONLY tenancy check that remains, and the one that matters: an id is a capability, and
	// another tenant that guesses one must not reach this work.
	if w.Tenant != tenant {
		return nil, ErrNotOwned
	}
	m.lastUse[id] = time.Now()
	return w, nil
}

// List returns the caller's workspaces, MOST RECENTLY USED FIRST.
//
// The ordering is not cosmetic: `List` is what a browser shows, and a map's iteration order would
// reshuffle the list on every request. `lastUse` already exists — it decides which checkout to reap
// when the disk fills — and it is exactly "when did anybody last touch this", which is the order a
// reader expects.
func (m *Manager) List(tenant, session string) []*Workspace {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Workspace, 0, len(m.areas))
	for _, w := range m.areas {
		if w.Tenant != tenant {
			continue
		}
		// An EMPTY session means "all of this tenant's", not "the ones with no session".
		//
		// The second reading is the literal one and it is useless: nothing creates a workspace
		// without a session, so the list was always empty. That is what an extension's own home screen
		// asked for — it is opened from a tab, not from a conversation, so it has no session to name
		// and showed "no workspace in this session" over data the caller could plainly see existed.
		//
		// The tenant is still enforced above, so this widens the view to the caller's OWN checkouts
		// and never to somebody else's.
		if session != "" && w.Session != session {
			continue
		}
		out = append(out, w)
	}
	// Newest first. A workspace whose lastUse is missing falls back to its creation time, so a
	// hand-built entry cannot sort as "older than everything".
	sort.SliceStable(out, func(i, j int) bool {
		return m.usedAt(out[i]).After(m.usedAt(out[j]))
	})
	return out
}

// usedAt is the last time this workspace was touched, falling back to when it was created.
func (m *Manager) usedAt(w *Workspace) time.Time {
	if t, ok := m.lastUse[w.ID]; ok {
		return t
	}
	return w.CreatedAt
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
