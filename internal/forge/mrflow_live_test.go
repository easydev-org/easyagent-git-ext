package forge

import (
	"context"
	"os"
	"testing"
)

// TestLiveMRFlow walks the WHOLE review workflow against a real Forgejo:
// list refs -> read a file -> open an MR -> comment on it -> read it back ->
// merge it. Each step uses the same client the tools use, so this is the closest
// thing to the extension's behaviour without an LLM in the loop.
//
// It needs a repository the token can PUSH to, which is why it is gated: the
// fixture must be a scratch repo, because the last step merges.
func TestLiveMRFlow(t *testing.T) {
	base := os.Getenv("GITEA_LIVE_URL")
	token := os.Getenv("GITEA_LIVE_TOKEN")
	remote := os.Getenv("GITEA_LIVE_PUSH_REMOTE")
	head := os.Getenv("GITEA_LIVE_HEAD") // a branch that already exists with commits
	if base == "" || token == "" || remote == "" || head == "" {
		t.Skip("set GITEA_LIVE_URL, GITEA_LIVE_TOKEN, GITEA_LIVE_PUSH_REMOTE and GITEA_LIVE_HEAD to run")
	}
	cl, err := New(base, token, remote)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	refs, err := cl.ListRefs(ctx)
	if err != nil {
		t.Fatalf("ListRefs: %v", err)
	}
	t.Logf("refs: %d", len(refs))
	for _, r := range refs {
		t.Logf("  %-7s %-30s %s", r.Kind, r.Name, r.Commit)
	}
	if len(refs) == 0 {
		t.Fatal("no refs; the fixture repo is empty")
	}

	f, err := cl.ReadFile(ctx, "README.md", "")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	t.Logf("README.md: %d bytes", f.Size)

	// Open an MR from the prepared head branch into main.
	pr, err := cl.OpenPullRequest(ctx, "main", head, "live smoke: review flow", "Opened by the live test.")
	if err != nil {
		t.Fatalf("OpenPullRequest: %v", err)
	}
	t.Logf("opened #%d %s -> %s  %s", pr.Index, pr.Head, pr.Base, pr.URL)

	// Comment on it, then read the thread back.
	cm, err := cl.Comment(ctx, pr.Index, "review note from the live test")
	if err != nil {
		t.Fatalf("Comment: %v", err)
	}
	t.Logf("comment %d posted", cm.ID)
	comments, err := cl.ListComments(ctx, pr.Index)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(comments) == 0 {
		t.Error("the comment we just posted is not in the thread")
	}
	for _, c := range comments {
		t.Logf("  comment %d by %s: %s", c.ID, c.Author, c.Body)
	}

	// List must show it as open.
	prs, err := cl.ListPullRequests(ctx, "open")
	if err != nil {
		t.Fatalf("ListPullRequests: %v", err)
	}
	found := false
	for _, p := range prs {
		if p.Index == pr.Index {
			found = true
		}
	}
	if !found {
		t.Errorf("MR #%d not in the open list", pr.Index)
	}

	// Merge it. This is the step that changes main, so it runs last.
	merged, err := cl.MergePullRequest(ctx, pr.Index, "", "", "", false)
	if err != nil {
		t.Fatalf("MergePullRequest: %v", err)
	}
	t.Logf("merged #%d state=%s", merged.Index, merged.State)
	// Gitea reports a merged MR as state=closed with merged=true (a merged PR is
	// not a separate state in its API), so "closed" here means success — what
	// matters is that it is no longer OPEN. MergePullRequest already fails loudly
	// when the MR is still open, which is the real regression guard.
	if merged.State == "open" {
		t.Errorf("state after merge = %q, the MR is still open", merged.State)
	}
}
