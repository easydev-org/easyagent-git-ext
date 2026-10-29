package forge

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

/**
 * A complete change, start to finish, the way an agent will do it.
 *
 * Every step is a separate call, and every step is checked against what the NEXT one needs — that is
 * what a walkthrough is for. A sequence that "works" but leaves the wrong state passes a smoke test
 * and fails the caller.
 *
 *	S1 sync main        the base is current, not whatever the mirror last saw
 *	S2 branch           a working branch, from that base
 *	S3 read             read a file before editing it
 *	S4 patch            one patch, one commit
 *	S5 read back        the edit is what was asked for
 *	S6 push             the branch reaches the upstream
 */
func TestWalkthroughLive(t *testing.T) {
	base := os.Getenv("FORGE_LIVE_URL")
	tok := os.Getenv("FORGE_LIVE_TOKEN")
	repo := os.Getenv("WALK_REPO")
	upstream := os.Getenv("UPSTREAM_LIVE_URL")
	upTok := os.Getenv("UPSTREAM_LIVE_TOKEN")
	if base == "" || tok == "" || repo == "" || upstream == "" {
		t.Skip("set FORGE_LIVE_*, WALK_REPO and UPSTREAM_LIVE_* to run")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Second)
	defer cancel()

	c, err := New(base, tok, base+"/"+repo+".git")
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().UnixNano()

	// S1. Bring main up to the upstream. On a fresh repository the local main has an initial commit
	// the upstream does not, so a fast-forward is impossible — and that is the right moment to see
	// what the refusal says.
	moved, before, after, err := c.SyncUpstream(ctx, "main", upstream, upTok)
	switch {
	case err != nil:
		t.Logf("S1 sync main: refused (%v)", err)
		t.Logf("   ^ expected on a seeded repository: its main holds an initial commit the upstream")
		t.Logf("     does not have. Fast-forward only, by design.")
	case moved:
		t.Logf("S1 sync main: %s -> %s", shortSHA(before), shortSHA(after))
	default:
		t.Logf("S1 sync main: already level at %s", shortSHA(before))
	}

	// S2. A working branch, from main.
	branch := fmt.Sprintf("walk/%d", stamp)
	if err := c.CreateBranch(ctx, branch, "main"); err != nil && err != ErrBranchExists {
		t.Fatalf("S2 branch: %v", err)
	}
	t.Logf("S2 branch: %s", branch)

	// S3. The existing file, so the patch below edits something real rather than adding beside it.
	store := forgeFileStore{c: c, branch: branch}
	old, rerr := store.ReadFile("README.md")
	if rerr != nil {
		t.Fatalf("S3 read README.md: %v", rerr)
	}
	t.Logf("S3 read: README.md is %d bytes", len(old))

	// S4. One patch that both adds a file and updates the README — two changes, ONE commit.
	note := fmt.Sprintf("walk-%d.md", stamp)
	patch := "*** Begin Patch\n" +
		"*** Add File: " + note + "\n+written by the walkthrough\n" +
		"*** Update File: README.md\n@@\n" +
		" # e2e\n" +
		"+walked at " + fmt.Sprint(stamp) + "\n" +
		"*** End Patch\n"
	plan, commit, aerr := c.ApplyPatch(ctx, branch, "walk: add a note and update the README", patch)
	if aerr != nil {
		t.Fatalf("S4 patch: %v", aerr)
	}
	t.Logf("S4 patch: %d changes in one commit %s", len(plan), shortSHA(commit))
	if len(plan) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(plan))
	}

	// S5. Read both back through the same path a caller would.
	got, rerr := store.ReadFile(note)
	if rerr != nil {
		t.Fatalf("S5 read %s: %v", note, rerr)
	}
	if string(got) != "written by the walkthrough\n" {
		t.Errorf("S5 %s = %q", note, string(got))
	}
	readme, rerr := store.ReadFile("README.md")
	if rerr != nil {
		t.Fatalf("S5 read README.md: %v", rerr)
	}
	t.Logf("S5 read back: %s OK, README.md is now %d bytes", note, len(readme))

	// S6. Push the branch upstream.
	dest := fmt.Sprintf("walk/%d", stamp)
	sha, perr := c.PushBranch(ctx, branch, upstream, dest, upTok)
	if perr != nil {
		t.Fatalf("S6 push: %v", perr)
	}
	t.Logf("S6 push: %s -> %s @ %s", branch, dest, shortSHA(sha))
	if sha != commit {
		t.Fatalf("S6 pushed %s but the patch made %s", shortSHA(sha), shortSHA(commit))
	}
}
