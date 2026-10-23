package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/easydev-org/easyagent-gitea-ext/internal/stage"
	"github.com/easylab-platform/easyvcs/object"
	"github.com/easylab-platform/easyvcs/revision"
	"github.com/easylab-platform/easyvcs/store"
)

// stageToolset returns a toolset wired to a real (SQLite) easyvcs store seeded with the
// given files.
//
// A real store, not a fake: these tools exist to work on database content, so faking the
// database would leave the interesting part untested.
func stageToolset(t *testing.T, files map[string]string) *toolset {
	t.Helper()
	dir := t.TempDir()
	cs, err := store.OpenDriver(store.DriverConfig{Kind: "sqlite", DSN: filepath.Join(dir, "vcs.db")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := cs.Init(); err != nil {
		t.Fatalf("init store: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	ns := store.RepoRef{Namespace: "tenant", Name: "t1"}
	repo, err := cs.Create(ns)
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}

	// Seed a base revision, then point main at it.
	ws := revision.NewWorkspace(repo)
	specs := make([]revision.FileChangeSpec, 0, len(files))
	for p, c := range files {
		specs = append(specs, revision.FileChangeSpec{Path: p, Content: []byte(c)})
	}
	_, rev, err := ws.CommitFromChanges(object.ID{}, specs, "seed",
		store.Author{Name: "seed", Email: "seed@example.invalid"}, "")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := repo.PutRef(&store.Ref{Name: "main", Kind: store.RefBranch, Target: rev.ID}); err != nil {
		t.Fatalf("put ref: %v", err)
	}

	area := stage.New(cs, func(string) store.RepoRef { return ns })
	return &toolset{deps: Deps{Stage: area}}
}

func TestStageToolsAreRegisteredOnlyWhenConfigured(t *testing.T) {
	// Without a store the names must not appear: a caller that found them would get an
	// error, and the client would advertise tools the deployment cannot serve.
	plain := Registry(Deps{})
	for _, name := range []string{"git-tree", "git-read", "git-write", "git-stage-diff"} {
		if _, ok := plain[name]; ok {
			t.Errorf("%s registered without a stage store", name)
		}
	}

	ts := stageToolset(t, map[string]string{"a.txt": "old\n"})
	withStage := Registry(ts.deps)
	for _, name := range []string{
		"git-tree", "git-read", "git-write", "git-remove",
		"git-stage-status", "git-stage-diff", "git-stage-reset",
	} {
		if _, ok := withStage[name]; !ok {
			t.Errorf("%s missing when a stage store IS configured", name)
		}
	}
}

func TestGitReadWithoutClone(t *testing.T) {
	ts := stageToolset(t, map[string]string{"README.md": "# hi\n", "src/a.go": "package a\n"})

	res, err := ts.stageRead(context.Background(), map[string]any{"path": "src/a.go"}, "s1", "t1")
	if err != nil {
		t.Fatalf("git-read: %v", err)
	}
	if res.Content != "package a\n" {
		t.Fatalf("content = %q", res.Content)
	}
}

func TestGitReadMissingPathIsNotFound(t *testing.T) {
	ts := stageToolset(t, map[string]string{"a.txt": "x\n"})
	_, err := ts.stageRead(context.Background(), map[string]any{"path": "nope.txt"}, "s1", "t1")
	if err == nil {
		t.Fatal("reading a missing path must fail")
	}
	// NOT a generic failure: the caller has to be able to tell "no such file" from
	// "the store is broken".
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("err = %v, want it to say the path does not exist", err)
	}
}

func TestGitWriteThenStatusThenDiff(t *testing.T) {
	ts := stageToolset(t, map[string]string{"a.txt": "old\n"})

	if _, err := ts.stageWrite(context.Background(),
		map[string]any{"path": "a.txt", "content": "new\n"}, "s1", "t1"); err != nil {
		t.Fatalf("git-write: %v", err)
	}

	// The write must be visible to a subsequent read — the caller sees its own work.
	res, err := ts.stageRead(context.Background(), map[string]any{"path": "a.txt"}, "s1", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Content != "new\n" {
		t.Fatalf("read after write = %q", res.Content)
	}

	status, err := ts.stageStatus(context.Background(), map[string]any{}, "s1", "t1")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(status.Content, "M a.txt") {
		t.Errorf("status = %q, want it to mark a.txt modified", status.Content)
	}

	diff, err := ts.stageDiff(context.Background(), map[string]any{}, "s1", "t1")
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if !strings.Contains(diff.Content, "-old") || !strings.Contains(diff.Content, "+new") {
		t.Errorf("diff = %q, want the change", diff.Content)
	}
}

func TestGitStageStatusEmpty(t *testing.T) {
	ts := stageToolset(t, map[string]string{"a.txt": "x\n"})
	res, err := ts.stageStatus(context.Background(), map[string]any{}, "s1", "t1")
	if err != nil {
		t.Fatal(err)
	}
	// A clean stage says so plainly; an empty string would read as a bug.
	if !strings.Contains(res.Content, "nothing staged") {
		t.Errorf("status = %q, want it to say nothing is staged", res.Content)
	}
}

func TestGitRemoveShowsAsDeletion(t *testing.T) {
	ts := stageToolset(t, map[string]string{"gone.txt": "bye\n"})
	if _, err := ts.stageRemove(context.Background(),
		map[string]any{"path": "gone.txt"}, "s1", "t1"); err != nil {
		t.Fatalf("git-remove: %v", err)
	}
	status, err := ts.stageStatus(context.Background(), map[string]any{}, "s1", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status.Content, "D gone.txt") {
		t.Errorf("status = %q, want D gone.txt", status.Content)
	}
}

func TestGitStageResetDiscards(t *testing.T) {
	ts := stageToolset(t, map[string]string{"a.txt": "old\n"})
	if _, err := ts.stageWrite(context.Background(),
		map[string]any{"path": "a.txt", "content": "changed\n"}, "s1", "t1"); err != nil {
		t.Fatal(err)
	}
	if _, err := ts.stageReset(context.Background(), map[string]any{}, "s1", "t1"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	// After a reset the base content is what a read returns again.
	res, err := ts.stageRead(context.Background(), map[string]any{"path": "a.txt"}, "s1", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Content != "old\n" {
		t.Fatalf("read after reset = %q, want the base content", res.Content)
	}
}

func TestGitTreeListsPaths(t *testing.T) {
	ts := stageToolset(t, map[string]string{"a.txt": "1\n", "dir/b.txt": "2\n"})
	res, err := ts.stageTree(context.Background(), map[string]any{}, "s1", "t1")
	if err != nil {
		t.Fatalf("tree: %v", err)
	}
	for _, want := range []string{"a.txt", "dir/b.txt"} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("tree missing %q:\n%s", want, res.Content)
		}
	}
}

func TestGitTreePrefixFilter(t *testing.T) {
	ts := stageToolset(t, map[string]string{"a.txt": "1\n", "dir/b.txt": "2\n"})
	res, err := ts.stageTree(context.Background(), map[string]any{"path": "dir/"}, "s1", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Content, "a.txt") && !strings.Contains(res.Content, "dir/b.txt") {
		t.Errorf("prefix filter dropped the wrong file: %q", res.Content)
	}
	if !strings.Contains(res.Content, "dir/b.txt") {
		t.Errorf("prefix filter dropped dir/b.txt: %q", res.Content)
	}
}

// Without a stage store the tools must say so, not panic or silently do nothing.
func TestStageToolWithoutStore(t *testing.T) {
	ts := &toolset{}
	if _, err := ts.stageTree(context.Background(), map[string]any{}, "s1", "t1"); err == nil {
		t.Fatal("a missing stage store must be reported")
	}
}
