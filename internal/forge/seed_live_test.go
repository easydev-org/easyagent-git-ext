package forge

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

/**
 * Seeds an empty local repository from the upstream, using the same fetch-and-push path the branch
 * creation uses.
 *
 * WHY NOT THE FORGE'S OWN MIGRATE API: it asks the forge to reach the upstream, and in this
 * deployment that request times out often enough to be unusable — it returned 422 and left an EMPTY
 * repository behind, which is the worst outcome because the repository looks fine until something
 * reads it. Doing it here means the failure is reported, and the bytes travel the same way every
 * other operation's do.
 */
func TestSeedEmptyRepoLive(t *testing.T) {
	base := os.Getenv("FORGE_LIVE_URL")
	tok := os.Getenv("FORGE_LIVE_TOKEN")
	repo := os.Getenv("SEED_REPO") // e.g. agentdev/e2e, created and empty
	upstream := os.Getenv("UPSTREAM_LIVE_URL")
	upTok := os.Getenv("UPSTREAM_LIVE_TOKEN")
	branch := os.Getenv("SEED_BRANCH")
	if base == "" || tok == "" || repo == "" || upstream == "" || branch == "" {
		t.Skip("set FORGE_LIVE_*, SEED_REPO, UPSTREAM_LIVE_URL and SEED_BRANCH to run")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	c, err := New(base, tok, base+"/"+repo+".git")
	if err != nil {
		t.Fatal(err)
	}
	sha, err := c.CreateBranchFromUpstream(ctx, branch, upstream, branch, upTok)
	if err != nil {
		t.Fatalf("seed %s from upstream %s: %v", branch, branch, err)
	}
	fmt.Printf("seeded %s at %s\n", branch, shortSHA(sha))
}
