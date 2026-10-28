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

// A workspace id is a capability, and the boundary is the TENANT.
//
// A session is NOT a boundary: the checkout is the tenant's own repository, and the same person
// browsing from another conversation — or from the extension's own tab, which has no session at all
// — must still be able to open it. Session-level ownership produced a list that showed a checkout
// and a click that refused to open it.
//
// What MUST still hold is the tenant check, which is the second assertion below.
func TestGetEnforcesTenantOwnership(t *testing.T) {
	m := newTestManager(t)
	// Register a workspace directly: Clone needs the network, and this test is about access.
	w := &Workspace{ID: "0123456789abcdef", Repo: "o/r", Dir: t.TempDir(),
		Tenant: "alice", Session: "s1", CreatedAt: time.Now()}
	m.areas[w.ID] = w
	m.lastUse[w.ID] = time.Now()

	if _, err := m.Get(w.ID, "alice", "s1"); err != nil {
		t.Fatalf("the owner should get it: %v", err)
	}
	// Another session of the SAME tenant: allowed, because the tenant owns the checkout.
	if _, err := m.Get(w.ID, "alice", "s2"); err != nil {
		t.Errorf("another session of the same tenant was refused: %v", err)
	}
	// A different tenant: refused, whatever session it names.
	if _, err := m.Get(w.ID, "bob", "s1"); err != ErrNotOwned {
		t.Errorf("another tenant got the workspace: err = %v, want ErrNotOwned", err)
	}
	if _, err := m.Get(w.ID, "bob", ""); err != ErrNotOwned {
		t.Errorf("another tenant got it by omitting the session: err = %v", err)
	}
}

// A malformed id must be rejected before it can name a filesystem path.
//
// It gets its OWN error, not ErrNoWorkspace: "expired, clone again" is the wrong advice for a typo,
// and the two cases need different answers.
func TestGetRejectsMalformedID(t *testing.T) {
	m := newTestManager(t)
	for _, bad := range []string{
		"", "../../etc", "0123456789abcde", "0123456789abcdef0",
		"0123456789ABCDEF", "0123456789abcde/", "0123456789abcdeg",
	} {
		if _, err := m.Get(bad, "alice", "s1"); err != ErrBadWorkspaceID {
			t.Errorf("Get(%q) = %v, want ErrBadWorkspaceID", bad, err)
		}
	}
}

// A well-formed id that is not registered is the EXPIRED case, and must say so.
//
// This is the message a user actually reads: the common cause is a service restart clearing the
// in-memory registry, not a typo, so it has to name that and the one next step.
func TestGetUnknownIDExplainsExpiry(t *testing.T) {
	m := newTestManager(t)
	_, err := m.Get("0123456789abcdef", "alice", "s1")
	if err != ErrNoWorkspace {
		t.Fatalf("err = %v, want ErrNoWorkspace", err)
	}
	for _, want := range []string{"expired", "git-ws-clone"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q does not mention %q", err.Error(), want)
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
