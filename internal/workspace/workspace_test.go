package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(t.TempDir(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// A workspace id is a capability. A session must not reach another session's work by guessing
// or reusing an id, so ownership is checked on EVERY access, not just the first.
func TestGetEnforcesOwnership(t *testing.T) {
	m := newTestManager(t)
	// Register a workspace directly: Clone needs the network, and this test is about access.
	w := &Workspace{ID: "0123456789abcdef", Repo: "o/r", Dir: t.TempDir(),
		Tenant: "alice", Session: "s1", CreatedAt: time.Now()}
	m.areas[w.ID] = w
	m.lastUse[w.ID] = time.Now()

	if _, err := m.Get(w.ID, "alice", "s1"); err != nil {
		t.Fatalf("the owner should get it: %v", err)
	}
	if _, err := m.Get(w.ID, "alice", "s2"); err != ErrNotOwned {
		t.Errorf("another session got the workspace: err = %v, want ErrNotOwned", err)
	}
	if _, err := m.Get(w.ID, "bob", "s1"); err != ErrNotOwned {
		t.Errorf("another tenant got the workspace: err = %v, want ErrNotOwned", err)
	}
}

// A malformed id must be rejected before it can name a filesystem path.
func TestGetRejectsMalformedID(t *testing.T) {
	m := newTestManager(t)
	for _, bad := range []string{
		"", "../../etc", "0123456789abcde", "0123456789abcdef0",
		"0123456789ABCDEF", "0123456789abcde/", "0123456789abcdeg",
	} {
		if _, err := m.Get(bad, "alice", "s1"); err != ErrNoWorkspace {
			t.Errorf("Get(%q) = %v, want ErrNoWorkspace", bad, err)
		}
	}
}

// A caller-supplied path must not escape the workspace. This is the check that stops
// "read ../../../etc/passwd" and "write into .git".
func TestSafePathRefusesEscapes(t *testing.T) {
	w := &Workspace{Dir: "/tmp/ws-test"}

	bad := []string{
		"../outside.txt",
		"a/../../x",
		"/etc/passwd",
		"",
		".git/config",
		".git",
	}
	for _, p := range bad {
		if _, err := w.safePath(p); err == nil {
			t.Errorf("safePath(%q) should have been refused", p)
		}
	}

	// Legitimate paths must pass — including a dotfile, which is not ".".
	good := []string{"a.txt", "dir/b.go", "a..b.txt", ".gitignore", ".github/workflows/ci.yml"}
	for _, p := range good {
		if _, err := w.safePath(p); err != nil {
			t.Errorf("safePath(%q) = %v, want ok", p, err)
		}
	}
}

// Writing, reading and removing files, plus the listing that excludes .git.
func TestFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	w := &Workspace{Dir: dir}

	if err := w.WriteFile("a/b.txt", []byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	got, err := w.ReadFile("a/b.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello\n" {
		t.Errorf("content = %q", got)
	}

	// .git is excluded from the listing so a caller does not try to patch a repository file.
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := w.ListFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "a/b.txt" {
		t.Errorf("ListFiles = %v, want [a/b.txt] only", files)
	}

	if err := w.RemoveFile("a/b.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.ReadFile("a/b.txt"); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("reading a removed file should say so: %v", err)
	}
}

// Abandoned workspaces are swept so the disk does not fill on a long-lived pod.
func TestSweepIdleRemovesOnlyStale(t *testing.T) {
	m := newTestManager(t)
	old := &Workspace{ID: "aaaaaaaaaaaaaaaa", Dir: t.TempDir(), Tenant: "t", Session: "s"}
	fresh := &Workspace{ID: "bbbbbbbbbbbbbbbb", Dir: t.TempDir(), Tenant: "t", Session: "s"}
	m.areas[old.ID] = old
	m.areas[fresh.ID] = fresh
	m.lastUse[old.ID] = time.Now().Add(-3 * time.Hour)
	m.lastUse[fresh.ID] = time.Now()

	if n := m.SweepIdle(time.Now()); n != 1 {
		t.Fatalf("swept %d, want 1", n)
	}
	if _, err := os.Stat(old.Dir); !os.IsNotExist(err) {
		t.Error("the stale workspace's directory should be gone")
	}
	if _, err := os.Stat(fresh.Dir); err != nil {
		t.Error("the fresh workspace should survive")
	}
	if _, err := m.Get(fresh.ID, "t", "s"); err != nil {
		t.Errorf("the fresh workspace should still be reachable: %v", err)
	}
}
