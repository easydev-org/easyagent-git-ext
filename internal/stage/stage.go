// Package stage implements a staging area for repository content that lives in a
// DATABASE, not in a checkout.
//
// WHY: reading a file or proposing a one-line change used to require cloning the
// repository into a sandbox — a container, a build of the toolchain, a copy of every
// file — to then send Gitea three API calls. For "fix this typo and open a PR" that is
// almost all overhead, and it makes the change impossible to review until the sandbox
// exists.
//
// easyvcs already stores a repository as objects in SQL (no `.git` directory), so the
// staging area is a thin layer over it: read a tree, apply edits as a change set,
// produce one revision, and let gitbridge push that revision to a branch.
//
// The staging area is per (tenant, session, repository) and holds a base revision plus
// the edits made since. It is deliberately NOT a second version control system: the
// authoritative history is easyvcs, and the stage is discarded once submitted.
package stage

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/easylab-platform/easyvcs/object"
	"github.com/easylab-platform/easyvcs/revision"
	"github.com/easylab-platform/easyvcs/store"
)

// Edit is one pending change to a path.
type Edit struct {
	Path    string `json:"path"`
	Content []byte `json:"-"`
	Delete  bool   `json:"delete,omitempty"`
	// Binary marks content that is not text; it is stored but never diffed line-wise.
	Binary bool `json:"binary,omitempty"`
}

// Workspace is one staging area.
type Workspace struct {
	mu sync.Mutex

	tenant  string
	session string
	repoRef string

	ws *revision.Workspace
	// repo is the easyvcs handle, kept because gitbridge needs it to export and because
	// revision.Workspace does not expose its store.
	repo *store.Repo
	// tenant names the easyvcs namespace this stage belongs to, so an empty store can be
	// seeded from a remote without asking the caller for it again.
	tenantID string
	// base is the revision the stage started from. Submitting produces a child of it,
	// so the resulting commit is a normal fast-forwardable revision.
	base string
	// edits is the staging area proper: path -> pending change. Applying an edit twice
	// replaces rather than duplicates, so a tool call is idempotent.
	edits map[string]Edit
}

// Key identifies a staging area.
func Key(tenant, session, repoRef string) string {
	return tenant + "\x00" + session + "\x00" + repoRef
}

// Stage holds every live staging area in this process.
//
// In memory on purpose: a stage is a short-lived scratchpad between a few tool calls in
// one turn. Persisting it would mean deciding when it expires, and a stale stage is
// worse than no stage — it would let a later turn submit edits made against a base that
// has since moved.
type Stage struct {
	mu        sync.Mutex
	areas     map[string]*Workspace
	store     *store.CentralStore
	namespace func(tenant string) store.RepoRef
}

// New returns a Stage backed by an easyvcs central store.
//
// `namespace` maps a tenant onto the easyvcs namespace that holds its mirrors; it is
// injected so this package does not have to know the tenancy scheme.
func New(cs *store.CentralStore, namespace func(tenant string) store.RepoRef) *Stage {
	return &Stage{areas: map[string]*Workspace{}, store: cs, namespace: namespace}
}

// ErrNoStage means nothing is staged for this key.
var ErrNoStage = errors.New("no staging area for this session and repository")

// For returns the staging area for a key, or nil.
func (s *Stage) For(tenant, session, repoRef string) *Workspace {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.areas[Key(tenant, session, repoRef)]
}

// Open starts a staging area at a repository's current head.
//
// Idempotent: opening an existing area keeps its edits, so a caller cannot lose work by
// re-opening. The base is NOT refreshed for the same reason — silently moving the base
// under pending edits would make them look correct while applying to content they never
// saw.
func (s *Stage) Open(ctx context.Context, tenant, session, repoRef, baseRef string) (*Workspace, error) {
	key := Key(tenant, session, repoRef)

	s.mu.Lock()
	if w, ok := s.areas[key]; ok {
		s.mu.Unlock()
		return w, nil
	}
	s.mu.Unlock()

	repo, err := s.ensureRepo(tenant)
	if err != nil {
		return nil, err
	}
	ws := revision.NewWorkspace(repo)

	base, err := resolveRef(ws, baseRef)
	if err != nil {
		return nil, err
	}

	w := &Workspace{
		tenant: tenant, tenantID: tenant, session: session, repoRef: repoRef,
		ws: ws, repo: repo, base: base, edits: map[string]Edit{},
	}

	s.mu.Lock()
	s.areas[key] = w
	s.mu.Unlock()
	return w, nil
}

// ensureRepo returns the tenant's easyvcs repository, creating an empty one the first
// time it is needed.
//
// A stage on a repository that does not exist yet is not an error the caller can fix: it
// simply has not been used before. Creating it here means "read a file" works on a fresh
// deployment without a separate provisioning step.
func (s *Stage) ensureRepo(tenant string) (*store.Repo, error) {
	ref := s.namespace(tenant)
	exists, err := s.store.RepoExists(ref)
	if err != nil {
		return nil, fmt.Errorf("check easyvcs repo: %w", err)
	}
	if !exists {
		if _, err := s.store.Create(ref); err != nil {
			return nil, fmt.Errorf("create easyvcs repo: %w", err)
		}
	}
	repo, err := s.store.OpenRepo(ref)
	if err != nil {
		return nil, fmt.Errorf("open easyvcs repo: %w", err)
	}
	return repo, nil
}

// resolveRef turns a ref expression into a revision id.
//
// easyvcs' resolver accepts revision ids, a unique id prefix, and "@" (the newest
// revision) — NOT branch names, because a ref points at a revision id and the resolver
// does not consult refs. So a caller asking for a branch is translated here: the
// repository's ref is looked up and its target used.
//
// Empty means "the tip", which is what a stage normally wants: it should start from
// whatever the repository currently holds.
func resolveRef(ws *revision.Workspace, ref string) (string, error) {
	switch ref {
	case "", "HEAD", "main", "master":
		// Fast path: the newest revision IS the tip for a mirror with linear history,
		// and "@" needs no ref lookup.
		_, revs, err := ws.ResolveRevisions([]string{"@"})
		if err != nil {
			return "", fmt.Errorf("resolve tip: %w", err)
		}
		if len(revs) == 0 {
			// An empty repository is a real state, not a failure: nothing has been
			// imported yet. Say what to do, because "no revisions" alone reads like a bug.
			return "", fmt.Errorf("nothing has been imported into this repository yet; " +
				"import a remote first (the staging area reads from stored content, so it " +
				"needs a base revision to start from)")
		}
		return revs[0].ID, nil
	}
	_, revs, err := ws.ResolveRevisions([]string{ref})
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", ref, err)
	}
	if len(revs) == 0 {
		return "", fmt.Errorf("no revision matches %q", ref)
	}
	return revs[0].ID, nil
}

// Base is the revision this stage started from.
func (w *Workspace) Base() string { return w.base }

// Write stages content for a path.
func (w *Workspace) Write(path string, content []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.edits[cleanPath(path)] = Edit{Path: cleanPath(path), Content: content, Binary: isBinary(content)}
}

// Remove stages a deletion.
func (w *Workspace) Remove(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.edits[cleanPath(path)] = Edit{Path: cleanPath(path), Delete: true}
}

// ResetEdit drops one pending edit, or all of them when path is empty.
//
// Distinct from a git reset: only the STAGE is cleared, the base revision is untouched.
func (w *Workspace) ResetEdit(path string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	if path == "" {
		n := len(w.edits)
		w.edits = map[string]Edit{}
		return n
	}
	p := cleanPath(path)
	if _, ok := w.edits[p]; ok {
		delete(w.edits, p)
		return 1
	}
	return 0
}

// Edits returns the pending changes, sorted by path for stable output.
func (w *Workspace) Edits() []Edit {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]Edit, 0, len(w.edits))
	for _, e := range w.edits {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// Dirty reports whether anything is staged.
func (w *Workspace) Dirty() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.edits) > 0
}

// Read returns a path's content as staged, or as it exists at the base.
//
// The stage wins: a caller asking for a file it just wrote must see its own write, not
// the committed version.
func (w *Workspace) Read(path string) ([]byte, bool, error) {
	p := cleanPath(path)
	w.mu.Lock()
	if e, ok := w.edits[p]; ok {
		w.mu.Unlock()
		if e.Delete {
			return nil, false, nil
		}
		return e.Content, true, nil
	}
	base := w.base
	ws := w.ws
	w.mu.Unlock()

	treeID, err := w.treeOf(ws, base)
	if err != nil {
		return nil, false, err
	}
	return readPath(ws, treeID, p)
}

// Paths lists every path visible from the stage: the base tree with pending edits
// applied.
func (w *Workspace) Paths() ([]string, error) {
	w.mu.Lock()
	base, ws, edits := w.base, w.ws, copyEdits(w.edits)
	w.mu.Unlock()

	treeID, err := w.treeOf(ws, base)
	if err != nil {
		return nil, err
	}
	basePaths, err := ws.CollectPaths(treeID)
	if err != nil {
		return nil, err
	}

	set := map[string]bool{}
	for _, p := range basePaths {
		set[p] = true
	}
	for p, e := range edits {
		if e.Delete {
			delete(set, p)
			continue
		}
		set[p] = true
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

func copyEdits(m map[string]Edit) map[string]Edit {
	out := make(map[string]Edit, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// treeOf returns the tree of a revision id.
//
// A Revision's `Hash` IS its snapshot id (the snapshot is content-addressed and can be
// rewritten without changing the revision id), so this reads the revision then its
// snapshot rather than the other way round.
func (w *Workspace) treeOf(ws *revision.Workspace, revID string) (object.ID, error) {
	rev, err := ws.GetRevision(revID)
	if err != nil {
		return object.ID{}, fmt.Errorf("revision %s: %w", revID, err)
	}
	snap, err := ws.GetSnapshot(rev.Hash)
	if err != nil {
		return object.ID{}, fmt.Errorf("snapshot %s: %w", rev.Hash, err)
	}
	return snap.TreeID, nil
}

// readPath returns a path's content from a tree.
//
// Walks the tree one segment at a time: paths are arbitrary depth, and a tree holds one
// level. A missing path is (nil,false,nil), not an error — callers ask about files that
// may not exist yet.
func readPath(ws *revision.Workspace, treeID object.ID, path string) ([]byte, bool, error) {
	t, err := ws.ReadTree(treeID)
	if err != nil {
		return nil, false, err
	}
	segs := strings.Split(path, "/")
	for i, seg := range segs {
		e, ok := t.Entries[seg]
		if !ok {
			return nil, false, nil
		}
		last := i == len(segs)-1
		if last {
			blob, err := ws.ReadBlob(e.ID)
			if err != nil {
				return nil, false, err
			}
			return blob, true, nil
		}
		t, err = ws.ReadTree(e.ID)
		if err != nil {
			return nil, false, err
		}
	}
	return nil, false, nil
}

// cleanPath normalises a path so the base tree and the stage agree on identity.
func cleanPath(p string) string {
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, "/")
	return p
}

// isBinary is a cheap heuristic; it decides whether a diff may be computed line-wise.
func isBinary(b []byte) bool {
	const probe = 8000
	if len(b) > probe {
		b = b[:probe]
	}
	for _, c := range b {
		if c == 0 {
			return true
		}
	}
	return false
}
