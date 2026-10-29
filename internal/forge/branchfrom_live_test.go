package forge

import (
	"context"
	"os"
	"testing"
	"time"
)

/**
 * Can a local branch start from an ARBITRARY branch on the upstream — not just one the mirror
 * already has?
 *
 * This is the question that decides how a retry works. If the answer is no, a caller whose pull
 * request conflicts can only cut from main; if it is yes, they can honour "base this on the branch
 * that is already open" without cloning anything.
 */
func TestCreateBranchFromUpstreamLive(t *testing.T) {
	base := os.Getenv("FORGE_LIVE_URL")
	tok := os.Getenv("FORGE_LIVE_TOKEN")
	repo := os.Getenv("LOOP_REPO")
	upstream := os.Getenv("UPSTREAM_LIVE_URL")
	upTok := os.Getenv("UPSTREAM_LIVE_TOKEN")
	src := os.Getenv("UPSTREAM_SRC_BRANCH") // a branch that exists ONLY upstream
	if base == "" || tok == "" || repo == "" || upstream == "" || src == "" {
		t.Skip("set FORGE_LIVE_*, LOOP_REPO, UPSTREAM_LIVE_* and UPSTREAM_SRC_BRANCH to run")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	c, err := New(base, tok, base+"/"+repo+".git")
	if err != nil {
		t.Fatal(err)
	}

	newBranch := "probe/from-upstream"
	sha, err := c.CreateBranchFromUpstream(ctx, newBranch, upstream, src, upTok)
	if err != nil {
		t.Fatalf("create from upstream %s: %v", src, err)
	}
	t.Logf("created %s at %s (copied from upstream %s)", newBranch, shortSHA(sha), src)

	// The branch must now exist locally and point at the upstream's commit — not at main.
	var got struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	if err := c.do(ctx, "GET", "/repos/"+c.path()+"/branches/"+esc(newBranch), nil, &got); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.Commit.ID != sha {
		t.Fatalf("branch points at %s, expected %s", shortSHA(got.Commit.ID), shortSHA(sha))
	}
}
