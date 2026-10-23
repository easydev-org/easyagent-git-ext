package stage

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/easylab-platform/easyvcs/object"
	"github.com/easylab-platform/easyvcs/revision"
	"github.com/easylab-platform/easyvcs/store"
)

// These tests exercise the stage against a REAL easyvcs store (SQLite on disk), because
// the value of this package is that it works on database content — a mock would prove
// nothing about the part that can actually break.

// newTestStore returns a CentralStore backed by a temp SQLite file plus a seeded repo.
//
// A file (not :memory:) because easyvcs' SQLite driver pins one connection and sets WAL
// through the DSN; an in-memory database per connection would give the test a store that
// silently loses writes.
func newTestStore(t *testing.T) (*store.CentralStore, store.RepoRef) {
	t.Helper()
	dir := t.TempDir()
	cs, err := store.OpenDriver(store.DriverConfig{
		Kind: "sqlite",
		DSN:  filepath.Join(dir, "vcs.db"),
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := cs.Init(); err != nil {
		t.Fatalf("init store: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs, store.RepoRef{Namespace: "test", Name: "repo"}
}

// seedRepo creates a repo whose HEAD holds the given files.
func seedRepo(t *testing.T, cs *store.CentralStore, ref store.RepoRef, files map[string]string) {
	t.Helper()
	repo, err := cs.Create(ref)
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	ws := revision.NewWorkspace(repo)

	changes := make([]revision.FileChangeSpec, 0, len(files))
	for path, content := range files {
		changes = append(changes, revision.FileChangeSpec{Path: path, Content: []byte(content)})
	}
	author := store.Author{Name: "seed", Email: "seed@example.invalid"}
	_, rev, err := ws.CommitFromChanges(object.ID{}, changes, "seed", author, "")
	if err != nil {
		t.Fatalf("seed commit: %v", err)
	}
	// Point the ref at the seed revision so a later resolve finds it.
	//
	// The Target is the REVISION id, not the snapshot hash: refs address stable identity,
	// while the snapshot behind a revision is rewritable.
	if err := repo.PutRef(&store.Ref{
		Name:   "main",
		Kind:   store.RefBranch,
		Target: rev.ID,
	}); err != nil {
		t.Fatalf("put ref: %v", err)
	}
}

func newStage(t *testing.T, cs *store.CentralStore, ref store.RepoRef) *Stage {
	t.Helper()
	return New(cs, func(string) store.RepoRef { return ref })
}

func TestReadFromBase(t *testing.T) {
	cs, ref := newTestStore(t)
	seedRepo(t, cs, ref, map[string]string{
		"README.md":     "# hello\n",
		"src/main.go":   "package main\n",
		"config/a.yaml": "a: 1\n",
	})
	st := newStage(t, cs, ref)

	w, err := st.Open(context.Background(), "t1", "s1", "repo", "main")
	if err != nil {
		t.Fatalf("open stage: %v", err)
	}

	// A nested path: the tree walk has to descend, not just look at the root.
	got, ok, err := w.Read("src/main.go")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !ok || string(got) != "package main\n" {
		t.Fatalf("src/main.go = %q (%v), want the seeded content", got, ok)
	}

	// A missing file is (nil,false,nil), not an error: callers ask about files that may
	// not exist yet.
	if _, ok, err := w.Read("nope.txt"); err != nil || ok {
		t.Fatalf("missing file = ok=%v err=%v, want ok=false err=nil", ok, err)
	}
}

func TestWriteIsVisibleToRead(t *testing.T) {
	cs, ref := newTestStore(t)
	seedRepo(t, cs, ref, map[string]string{"a.txt": "old\n"})
	st := newStage(t, cs, ref)
	w, err := st.Open(context.Background(), "t1", "s1", "repo", "main")
	if err != nil {
		t.Fatal(err)
	}

	w.Write("a.txt", []byte("new\n"))

	// The stage wins over the base: a caller that just wrote must see its own write.
	got, ok, err := w.Read("a.txt")
	if err != nil || !ok {
		t.Fatalf("read after write: %v %v", ok, err)
	}
	if string(got) != "new\n" {
		t.Fatalf("read = %q, want the staged content", got)
	}
}

func TestPathsMergesBaseAndStage(t *testing.T) {
	cs, ref := newTestStore(t)
	seedRepo(t, cs, ref, map[string]string{"keep.txt": "k\n", "gone.txt": "g\n"})
	st := newStage(t, cs, ref)
	w, err := st.Open(context.Background(), "t1", "s1", "repo", "main")
	if err != nil {
		t.Fatal(err)
	}

	w.Write("added.txt", []byte("a\n"))
	w.Remove("gone.txt")

	paths, err := w.Paths()
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(paths, ",")
	if !strings.Contains(joined, "keep.txt") {
		t.Error("unmodified base file missing")
	}
	if !strings.Contains(joined, "added.txt") {
		t.Error("staged addition missing")
	}
	if strings.Contains(joined, "gone.txt") {
		t.Error("staged deletion still listed")
	}
}

func TestStatusClassifiesChanges(t *testing.T) {
	cs, ref := newTestStore(t)
	seedRepo(t, cs, ref, map[string]string{
		"keep.txt": "same\n",
		"mod.txt":  "before\n",
		"del.txt":  "bye\n",
	})
	st := newStage(t, cs, ref)
	w, err := st.Open(context.Background(), "t1", "s1", "repo", "main")
	if err != nil {
		t.Fatal(err)
	}

	w.Write("new.txt", []byte("fresh\n"))
	w.Write("mod.txt", []byte("after\n"))
	w.Remove("del.txt")

	files, err := w.Status()
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]FileStatus{}
	for _, f := range files {
		byPath[f.Path] = f
	}
	if byPath["new.txt"].Status != "added" {
		t.Errorf("new.txt status = %q, want added", byPath["new.txt"].Status)
	}
	if byPath["mod.txt"].Status != "modified" {
		t.Errorf("mod.txt status = %q, want modified", byPath["mod.txt"].Status)
	}
	if byPath["del.txt"].Status != "deleted" {
		t.Errorf("del.txt status = %q, want deleted", byPath["del.txt"].Status)
	}
	// Only the changed files are listed: an unmodified file is not a change.
	if _, ok := byPath["keep.txt"]; ok {
		t.Error("keep.txt reported as changed")
	}
}

func TestDiffCoversStagedChanges(t *testing.T) {
	cs, ref := newTestStore(t)
	seedRepo(t, cs, ref, map[string]string{"a.txt": "one\ntwo\n"})
	st := newStage(t, cs, ref)
	w, err := st.Open(context.Background(), "t1", "s1", "repo", "main")
	if err != nil {
		t.Fatal(err)
	}
	w.Write("a.txt", []byte("one\nTWO\n"))

	diff, err := w.Diff()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "--- a/a.txt") || !strings.Contains(diff, "+++ b/a.txt") {
		t.Errorf("diff missing headers:\n%s", diff)
	}
	if !strings.Contains(diff, "-two") || !strings.Contains(diff, "+TWO") {
		t.Errorf("diff missing the change:\n%s", diff)
	}
}

func TestCommitProducesARevision(t *testing.T) {
	cs, ref := newTestStore(t)
	seedRepo(t, cs, ref, map[string]string{"a.txt": "old\n"})
	st := newStage(t, cs, ref)
	w, err := st.Open(context.Background(), "t1", "s1", "repo", "main")
	if err != nil {
		t.Fatal(err)
	}
	w.Write("a.txt", []byte("new\n"))

	snap, rev, err := w.Commit(CommitParams{
		Description: "change a",
		Author:      store.Author{Name: "agent", Email: "agent@example.invalid"},
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if rev == nil || rev.ID == "" {
		t.Fatal("no revision produced")
	}
	if snap == nil || snap.TreeID == (object.ID{}) {
		t.Fatal("no snapshot produced")
	}
	if snap.Description != "change a" {
		t.Errorf("description = %q", snap.Description)
	}

	// The new snapshot must have the seed snapshot as its parent: the change lands ON TOP
	// of history rather than replacing it.
	if len(snap.Parents) != 1 {
		t.Fatalf("parents = %v, want exactly one", snap.Parents)
	}
}

func TestCommitRefusesEmptyStage(t *testing.T) {
	cs, ref := newTestStore(t)
	seedRepo(t, cs, ref, map[string]string{"a.txt": "old\n"})
	st := newStage(t, cs, ref)
	w, err := st.Open(context.Background(), "t1", "s1", "repo", "main")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := w.Commit(CommitParams{}); err == nil {
		t.Fatal("committing an empty stage must fail rather than create a no-op revision")
	}
}

func TestResetEdit(t *testing.T) {
	cs, ref := newTestStore(t)
	seedRepo(t, cs, ref, map[string]string{"a.txt": "old\n"})
	st := newStage(t, cs, ref)
	w, err := st.Open(context.Background(), "t1", "s1", "repo", "main")
	if err != nil {
		t.Fatal(err)
	}
	w.Write("a.txt", []byte("new\n"))
	w.Write("b.txt", []byte("b\n"))

	if n := w.ResetEdit("a.txt"); n != 1 {
		t.Fatalf("reset a.txt removed %d edits, want 1", n)
	}
	if !w.Dirty() {
		t.Error("b.txt should still be staged")
	}
	if n := w.ResetEdit(""); n != 1 {
		t.Fatalf("reset all removed %d edits, want 1", n)
	}
	if w.Dirty() {
		t.Error("stage should be empty")
	}
}

// Opening an existing stage must NOT reset it: re-opening is what a later tool call in
// the same turn does, and losing the edits there would be silent data loss.
func TestOpenIsIdempotent(t *testing.T) {
	cs, ref := newTestStore(t)
	seedRepo(t, cs, ref, map[string]string{"a.txt": "old\n"})
	st := newStage(t, cs, ref)
	w1, err := st.Open(context.Background(), "t1", "s1", "repo", "main")
	if err != nil {
		t.Fatal(err)
	}
	w1.Write("a.txt", []byte("staged\n"))

	w2, err := st.Open(context.Background(), "t1", "s1", "repo", "main")
	if err != nil {
		t.Fatal(err)
	}
	if w1 != w2 {
		t.Fatal("re-opening returned a different workspace; edits would be lost")
	}
	got, _, _ := w2.Read("a.txt")
	if string(got) != "staged\n" {
		t.Fatalf("staged edit lost on re-open: %q", got)
	}
}

// Separate sessions must not see each other's edits.
func TestStagesAreIsolatedPerSession(t *testing.T) {
	cs, ref := newTestStore(t)
	seedRepo(t, cs, ref, map[string]string{"a.txt": "old\n"})
	st := newStage(t, cs, ref)

	w1, err := st.Open(context.Background(), "t1", "session-a", "repo", "main")
	if err != nil {
		t.Fatal(err)
	}
	w2, err := st.Open(context.Background(), "t1", "session-b", "repo", "main")
	if err != nil {
		t.Fatal(err)
	}
	w1.Write("a.txt", []byte("from A\n"))

	got, _, _ := w2.Read("a.txt")
	if string(got) != "old\n" {
		t.Fatalf("session-b sees session-a's edit: %q", got)
	}
}

var _ = fmt.Sprintf
