package forge

import (
	"context"
	"os"
	"testing"
	"time"
)

/**
 * CommitFiles against a REAL forge, when one is configured.
 *
 * Skipped unless FORGE_LIVE_URL and FORGE_LIVE_TOKEN are set: the unit tests cover the shape of the
 * calls, but only a real forge says whether the four-step sequence actually produces one commit with
 * every file in it. That question cannot be answered by a fake.
 *
 *	FORGE_LIVE_URL=http://forgejo.dev.svc.cluster.local
 *	FORGE_LIVE_TOKEN=... FORGE_LIVE_REPO=agentdev/scratch go test ./internal/forge -run Live
 */
func TestCommitFilesLive(t *testing.T) {
	base := os.Getenv("FORGE_LIVE_URL")
	tok := os.Getenv("FORGE_LIVE_TOKEN")
	repo := os.Getenv("FORGE_LIVE_REPO")
	if base == "" || tok == "" || repo == "" {
		t.Skip("set FORGE_LIVE_URL, FORGE_LIVE_TOKEN and FORGE_LIVE_REPO to run")
	}
	c, err := New(base, tok, base+"/"+repo+".git")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	branch := "live-probe"
	// Start from main so the branch exists; a failure here means the repository is not where the
	// environment said it was, which is worth failing on rather than skipping.
	if err := c.CreateBranch(ctx, branch, "main"); err != nil && err != ErrBranchExists {
		t.Fatalf("create branch: %v", err)
	}

	sha, err := c.CommitFiles(ctx, branch, "probe: two files in one commit", []TreeChange{
		{Path: "a.txt", Content: []byte("alpha\n")},
		{Path: "dir/b.txt", Content: []byte("beta\n")},
	}, nil)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if sha == "" {
		t.Fatal("no commit sha")
	}
	t.Logf("commit %s", sha)
}
