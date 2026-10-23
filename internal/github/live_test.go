package github

import (
	"context"
	"os"
	"testing"
	"time"
)

// LIVE tests: they run against the real GitHub API and are skipped unless GH_LIVE=1.
//
// Worth keeping as tests rather than a throwaway script: a fake HTTP server proves the parsing,
// not that GitHub answers the way the parser expects. These are the checks that catch "the
// endpoint moved" and "the field is nested differently than I assumed".

func liveClient(t *testing.T, remote string, withToken bool) *Client {
	t.Helper()
	if os.Getenv("GH_LIVE") != "1" {
		t.Skip("set GH_LIVE=1 to run against the real GitHub API")
	}
	opts := Options{Remote: remote}
	if withToken {
		opts.Token = os.Getenv("GH_TOKEN")
		if opts.Token == "" {
			t.Skip("GH_TOKEN is required for this case")
		}
	}
	c, err := New(opts)
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	return c
}

func liveCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// The extension's core promise: read a public repository with NO credential.
func TestLiveAnonymousRead(t *testing.T) {
	c := liveClient(t, "https://github.com/octocat/Hello-World", false)
	ctx := liveCtx(t)

	info, err := c.Info(ctx)
	if err != nil {
		t.Fatalf("info: %v", err)
	}
	if info.FullName != "octocat/Hello-World" || info.Private {
		t.Errorf("info = %+v", info)
	}
	if info.DefaultBranch == "" {
		t.Error("no default branch")
	}

	tree, err := c.Tree(ctx, "")
	if err != nil {
		t.Fatalf("tree: %v", err)
	}
	if len(tree) == 0 {
		t.Error("empty tree for a repository that has files")
	}

	f, err := c.ReadFile(ctx, tree[0].Path, "")
	if err != nil {
		t.Fatalf("read %s: %v", tree[0].Path, err)
	}
	if f.Path == "" {
		t.Error("file has no path")
	}
}

func TestLiveIdentityAndScopes(t *testing.T) {
	c := liveClient(t, "https://github.com/easydev-org/easyagent", true)
	ctx := liveCtx(t)

	id, sc, err := c.IdentityAndScopes(ctx)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	if id.Login == "" {
		t.Error("no login")
	}
	// Either a classic scope list or the fine-grained marker; never both empty AND not
	// fine-grained, which would mean the detection silently failed.
	if len(sc.Classic) == 0 && !sc.FineGrained {
		t.Errorf("scopes = %+v: neither scopes nor the fine-grained marker", sc)
	}
	t.Logf("login=%s classic=%v fine_grained=%v expires=%q", id.Login, sc.Classic, sc.FineGrained, sc.ExpiresAt)
}

func TestLiveOrgsAndRepos(t *testing.T) {
	c := liveClient(t, "https://github.com/easydev-org/easyagent", true)
	ctx := liveCtx(t)

	orgs, err := c.Orgs(ctx)
	if err != nil {
		t.Fatalf("orgs: %v", err)
	}
	names := make([]string, 0, len(orgs))
	for _, o := range orgs {
		names = append(names, o.Login)
	}
	t.Logf("orgs: %v", names)

	repos, err := c.ListOrgRepos(ctx, "easydev-org", "all")
	if err != nil {
		t.Fatalf("org repos: %v", err)
	}
	if len(repos) == 0 {
		t.Errorf("no repositories listed for easydev-org")
	}
	for _, r := range repos {
		if r.FullName == "" || r.DefaultBranch == "" {
			t.Errorf("incomplete repo entry: %+v", r)
		}
	}
}

func TestLiveReleasesAndPackages(t *testing.T) {
	c := liveClient(t, "https://github.com/easydev-org/easyagent", true)
	ctx := liveCtx(t)

	rel, err := c.Releases(ctx, 5)
	if err != nil {
		t.Fatalf("releases: %v", err)
	}
	t.Logf("releases: %d", len(rel))

	pkgs, err := c.ListOrgPackages(ctx, "easydev-org", "container")
	if err != nil {
		t.Fatalf("packages: %v", err)
	}
	t.Logf("container packages: %d", len(pkgs))
	if len(pkgs) == 0 {
		return
	}
	// The versions endpoint has a different path shape; exercising it is the point.
	vers, err := c.PackageVersions(ctx, "easydev-org", "container", pkgs[0].Name)
	if err != nil {
		t.Fatalf("package versions for %s: %v", pkgs[0].Name, err)
	}
	t.Logf("%s: %d versions", pkgs[0].Name, len(vers))
}
