package ghclient

import (
	"context"
	"os"
	"testing"
	"time"
)

// ListIssues against a real repository, with the state made explicit.
//
// Written because the tool-level test reported "no issues" for a repository that plainly had one.
// A repository whose issues are almost all pull requests is the interesting case: the two share a
// numbering space, so a filter that drops pull requests can empty the list by accident.
//
// GH_E2E=1 GH_TOKEN=... GH_E2E_REPO=owner/repo go test ./internal/ghclient -run TestLiveListIssues
func TestLiveListIssues(t *testing.T) {
	if os.Getenv("GH_E2E") != "1" {
		t.Skip("set GH_E2E=1 (and GH_TOKEN, GH_E2E_REPO)")
	}
	token, repo := os.Getenv("GH_TOKEN"), os.Getenv("GH_E2E_REPO")
	if token == "" || repo == "" {
		t.Fatal("GH_TOKEN and GH_E2E_REPO are required")
	}
	c, err := New(Options{Remote: "https://github.com/" + repo, Token: token})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	for _, state := range []string{"open", "closed", "all"} {
		items, lerr := c.ListIssues(ctx, ListIssuesOptions{State: state, Limit: 50})
		if lerr != nil {
			t.Fatalf("state=%s: %v", state, lerr)
		}
		t.Logf("state=%-6s -> %d issue(s)", state, len(items))
		for _, i := range items {
			t.Logf("   #%d %s [%s] PR=%v", i.Number, i.Title, i.State, i.IsPullRequest)
		}
	}
}
