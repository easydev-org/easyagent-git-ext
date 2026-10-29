package forge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

/**
 * The fast-forward check, against real history in a temporary directory.
 *
 * This guard stands between "sync the protected branch" and "silently discard a commit somebody
 * pushed there". It normally passes — a protected branch only moves by merges upstream — but the
 * failure it prevents is unrecoverable, so it is worth testing directly.
 *
 * A temporary DIRECTORY rather than in-memory storage: committing needs a worktree to put files in,
 * and an in-memory store has no filesystem to back one.
 */

// buildRepo makes a repository with three commits on main, returning their shas in order.
func buildRepo(t *testing.T) (*git.Repository, []string) {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	var shas []string
	for _, name := range []string{"one.txt", "two.txt", "three.txt"} {
		if err := writeFile(dir, name, "content of "+name+"\n"); err != nil {
			t.Fatal(err)
		}
		if _, err := wt.Add(name); err != nil {
			t.Fatal(err)
		}
		h, cerr := wt.Commit("commit "+name, &git.CommitOptions{
			Author: &object.Signature{Name: "t", Email: "t@example"},
		})
		if cerr != nil {
			t.Fatal(cerr)
		}
		shas = append(shas, h.String())
	}
	return repo, shas
}

// An ancestor must be accepted: that is the normal case.
func TestAssertFastForwardAcceptsAnAncestor(t *testing.T) {
	repo, shas := buildRepo(t)
	setRemoteRef(t, repo, "origin", "main", shas[2])

	if err := assertFastForward(repo, "origin", "main", shas[0]); err != nil {
		t.Fatalf("an ancestor should be accepted: %v", err)
	}
	// The current head is trivially its own ancestor — syncing twice must stay a no-op.
	if err := assertFastForward(repo, "origin", "main", shas[2]); err != nil {
		t.Fatalf("the head itself should be accepted: %v", err)
	}
}

// A commit that is not in the upstream's history must be refused, and the message must state the
// rule — a reader has to know that a fast-forward is required, not merely that something failed.
func TestAssertFastForwardRefusesAStranger(t *testing.T) {
	repo, shas := buildRepo(t)
	setRemoteRef(t, repo, "origin", "main", shas[2])

	err := assertFastForward(repo, "origin", "main", strings.Repeat("a", 40))
	if err == nil {
		t.Fatal("a commit the upstream does not have should be refused")
	}
	if !strings.Contains(err.Error(), "Fast-forward only") {
		t.Errorf("the refusal should state the rule, got: %v", err)
	}
}

// A missing remote ref is an ERROR, not a pass: it means the fetch did not do what this assumed, and
// returning nil would let an unchecked push through.
func TestAssertFastForwardNeedsTheFetchedRef(t *testing.T) {
	repo, shas := buildRepo(t)
	if err := assertFastForward(repo, "origin", "main", shas[0]); err == nil {
		t.Fatal("a missing remote ref should be an error")
	}
}

func setRemoteRef(t *testing.T, repo *git.Repository, remote, branch, sha string) {
	t.Helper()
	ref := plumbing.NewHashReference(plumbing.NewRemoteReferenceName(remote, branch), plumbing.NewHash(sha))
	if err := repo.Storer.SetReference(ref); err != nil {
		t.Fatal(err)
	}
}

// writeFile puts one file in the worktree directory.
func writeFile(dir, name, content string) error {
	return os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)
}
