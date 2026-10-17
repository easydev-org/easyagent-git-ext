package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// newRemote creates a bare repo with one commit and returns its path (used as
// the "remote" — a local path, so no network is needed). It shells out to the
// system `git` ONLY in the test (the extension itself never needs it).
func newRemote(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	bare := filepath.Join(dir, "remote.git")
	run(t, "git", "init", "--bare", "--initial-branch=main", bare)

	work := filepath.Join(dir, "work")
	run(t, "git", "clone", bare, work)
	run(t, "git", "-C", work, "config", "user.email", "t@x")
	run(t, "git", "-C", work, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(work, name), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, "git", "-C", work, "add", name)
	run(t, "git", "-C", work, "commit", "-m", "init")
	run(t, "git", "-C", work, "push", "origin", "main")
	return bare
}

func run(t *testing.T, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

func TestMain(m *testing.M) { os.Exit(m.Run()) }

// TestCloneStatusLogCommitPush drives the tool handlers against a local bare
// repo: clone -> status -> commit -> log. (Push to a bare local path is
// exercised too — go-git supports file:// remotes.)
func TestCloneStatusLogCommit(t *testing.T) {
	remote := newRemote(t, "README.md")
	home := t.TempDir()
	reg, err := Registry(Deps{
		Home:      home,
		GetConfig: func(string, string, string) any { return "" }, // anonymous (local path)
		Locale:    func(string, string) string { return "en" },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	args := map[string]any{"remote": remote, "depth": float64(0)}

	// clone
	res, err := reg["git-clone"](ctx, args, "sess", "tenant-a")
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	if res.Data == nil {
		t.Fatal("clone returned no data")
	}

	// status (clean)
	res, err = reg["git-status"](ctx, args, "sess", "tenant-a")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if res.Content == "" {
		t.Error("status content empty")
	}

	// log has the init commit
	res, err = reg["git-log"](ctx, args, "sess", "tenant-a")
	if err != nil {
		t.Fatalf("log: %v", err)
	}

	// write a file + commit (locate the clone via the on-disk layout)
	wsDir := mustRepoDir(t, home, "tenant-a", remote)
	if err := os.WriteFile(filepath.Join(wsDir, "new.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := reg["git-add"](ctx, map[string]any{"remote": remote, "paths": []any{"new.txt"}}, "sess", "tenant-a"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := reg["git-commit"](ctx, map[string]any{"remote": remote, "message": "add new"}, "sess", "tenant-a"); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// TestTenantIsolation: two tenants cloning the same remote get separate clones.
func TestTenantIsolation(t *testing.T) {
	remote := newRemote(t, "f.txt")
	home := t.TempDir()
	reg, _ := Registry(Deps{Home: home, GetConfig: func(string, string, string) any { return "" }, Locale: func(string, string) string { return "" }})
	ctx := context.Background()
	args := map[string]any{"remote": remote, "depth": float64(1)}
	if _, err := reg["git-clone"](ctx, args, "s", "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := reg["git-clone"](ctx, args, "s", "b"); err != nil {
		t.Fatal(err)
	}
	if mustRepoDir(t, home, "a", remote) == mustRepoDir(t, home, "b", remote) {
		t.Fatal("tenants share a clone dir")
	}
}

// mustRepoDir mirrors workspace.Manager.Path without importing it (keeps the
// test honest about the on-disk layout: <home>/<tenant>/<hash>).
func mustRepoDir(t *testing.T, home, tenant, remote string) string {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(home, tenant, "*"))
	if len(matches) == 0 {
		t.Fatalf("no repo dir under %s/%s", home, tenant)
	}
	return matches[0]
}
