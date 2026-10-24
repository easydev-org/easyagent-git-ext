package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// A real repository with one commit, so there is a HEAD to diff against.
func newGitWorkspace(t *testing.T) *Workspace {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "edit.txt"), []byte("old line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gone.txt"), []byte("bye\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("initial", &git.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@example.com"},
	}); err != nil {
		t.Fatal(err)
	}
	return &Workspace{Dir: dir}
}

// The three shapes a change can take must all appear in the diff: a modified file, a new file,
// and a deleted one. Missing any of them would mean a reviewer cannot see part of the change.
func TestDiffShowsAllThreeChangeKinds(t *testing.T) {
	w := newGitWorkspace(t)
	if err := w.WriteFile("edit.txt", []byte("new line\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteFile("added.txt", []byte("brand new\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.RemoveFile("gone.txt"); err != nil {
		t.Fatal(err)
	}

	d, err := w.DiffGoGit(DiffOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"edit.txt", "-old line", "+new line", // modified
		"added.txt", "+brand new", // added
		"gone.txt", "-bye", // deleted
	} {
		if !strings.Contains(d, want) {
			t.Errorf("diff is missing %q:\n%s", want, d)
		}
	}
	if strings.Contains(d, "keep.txt") {
		t.Errorf("an unchanged file should not appear in the diff:\n%s", d)
	}
}

// An unchanged workspace must produce an EMPTY diff, not a header with nothing under it.
func TestDiffIsEmptyWhenClean(t *testing.T) {
	w := newGitWorkspace(t)
	d, err := w.DiffGoGit(DiffOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(d) != "" {
		t.Errorf("a clean workspace produced a diff:\n%s", d)
	}
}

// The path filter must show one file and hide the others.
func TestDiffPathFilter(t *testing.T) {
	w := newGitWorkspace(t)
	if err := w.WriteFile("edit.txt", []byte("changed\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteFile("added.txt", []byte("new\n")); err != nil {
		t.Fatal(err)
	}

	d, err := w.DiffGoGit(DiffOptions{Path: "edit.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d, "edit.txt") {
		t.Errorf("the requested file is missing:\n%s", d)
	}
	if strings.Contains(d, "added.txt") {
		t.Errorf("an unrequested file appears:\n%s", d)
	}
}

// A path that escapes the workspace must be refused before it can be read.
func TestDiffRefusesEscapingPath(t *testing.T) {
	w := newGitWorkspace(t)
	if _, err := w.DiffGoGit(DiffOptions{Path: "../../etc/passwd"}); err == nil {
		t.Fatal("an escaping path should be refused")
	}
}
