package forge

import (
	"context"
	"os"
	"testing"
	"time"
)

/**
 * A patch against a REAL forge: plan, commit, and read back.
 *
 * The unit tests below cover the grammar with a fake store. What they cannot say is whether a patch
 * reaches the repository as ONE commit with the right content — that needs a forge.
 */
func TestApplyPatchLive(t *testing.T) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	branch := "patch-probe"
	if err := c.CreateBranch(ctx, branch, "main"); err != nil && err != ErrBranchExists {
		t.Fatalf("create branch: %v", err)
	}

	// Two adds in ONE patch: the point is that both land in a single commit. An update would need the
	// file's exact current content, which this test has no reason to depend on.
	patch := "*** Begin Patch\n" +
		"*** Add File: p1.txt\n+one\n" +
		"*** Add File: p2.txt\n+two\n" +
		"*** End Patch\n"

	plan, sha, err := c.ApplyPatch(ctx, branch, "probe: patch as one commit", patch)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(plan) != 2 {
		t.Fatalf("plan has %d changes, want 2", len(plan))
	}
	if sha == "" {
		t.Fatal("no commit sha")
	}
	t.Logf("commit %s, %d change(s)", sha, len(plan))

	// Read both files back through the same client the planner uses, which is also the only way to
	// confirm the commit contents are what the patch said.
	store := forgeFileStore{c: c, branch: branch}
	for _, want := range []struct{ path, content string }{
		{"p1.txt", "one\n"}, {"p2.txt", "two\n"},
	} {
		got, rerr := store.ReadFile(want.path)
		if rerr != nil {
			t.Errorf("read %s: %v", want.path, rerr)
			continue
		}
		if string(got) != want.content {
			t.Errorf("%s = %q, want %q", want.path, string(got), want.content)
		}
	}
}
