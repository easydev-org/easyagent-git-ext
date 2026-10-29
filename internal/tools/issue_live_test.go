package tools

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	easybus "github.com/easydev-org/easybus-sdk"
	"github.com/easydev-org/easybus-sdk/extension"
)

// Issues and comments against real GitHub: create → read back → comment → verify both.
//
// The point is the round trip. A comment tool that returns 200 but writes nothing looks identical
// to one that works, so every write is read back through a separate tool.
func TestE2EIssuesAndComments(t *testing.T) {
	if os.Getenv("GH_E2E") != "1" {
		t.Skip("set GH_E2E=1 (and GH_TOKEN, GH_E2E_REPO)")
	}
	token, repo := os.Getenv("GH_TOKEN"), os.Getenv("GH_E2E_REPO")
	if token == "" || repo == "" {
		// A MISSING TOKEN IS A SKIP, not a failure. GH_E2E=1 says "run the live suite"; whether this
		// deployment has a token is a property of the deployment. Failing here would turn an
		// unconfigured secret into a red build that says nothing about the code.
		t.Skip("set GH_TOKEN and GH_E2E_REPO to run the live suite")
	}

	reg := Registry(Deps{
		GetConfig: func(name, session, tenant string) any {
			if name == "token" {
				return token
			}
			return nil
		},
		Locale: func(string, string) string { return "en" },
	})
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	tenant, session := "alice", "issuese2e"
	ctx = extension.WithGrants(ctx, []*easybus.Grant{
		{Key: PermMaintainer, Params: easybus.Struct(map[string]any{"repo": repo})},
		{Key: PermMaintainer, Params: easybus.Struct(map[string]any{"repo": repo})},
	})

	marker := time.Now().Format("150405")
	t.Log("1. gh-issue-create")
	res, err := reg["gh-issue-create"](ctx, map[string]any{
		"repo": repo, "title": "e2e: issue tool probe " + marker,
		"body": "Created by the end-to-end test.", "labels": []string{"e2e-probe"},
	}, session, tenant)
	if err != nil {
		t.Fatalf("gh-issue-create: %v", err)
	}
	t.Logf("%s", indent(res.Content))
	num, _ := res.Data.(map[string]any)["issue"].(int)
	if num == 0 {
		t.Fatalf("no issue number: %+v", res.Data)
	}

	t.Logf("2. gh-issue-comment on #%d", num)
	res, err = reg["gh-issue-comment"](ctx, map[string]any{
		"repo": repo, "issue_id": num, "body": "first comment from the probe",
	}, session, tenant)
	if err != nil {
		t.Fatalf("gh-issue-comment: %v", err)
	}
	t.Logf("%s", indent(res.Content))

	t.Logf("3. gh-issue-show on #%d — the issue itself", num)
	res, err = reg["gh-issue-show"](ctx, map[string]any{"repo": repo, "issue_id": num}, session, tenant)
	if err != nil {
		t.Fatalf("gh-issue-show: %v", err)
	}
	t.Logf("%s", indent(res.Content))
	if !strings.Contains(res.Content, marker) {
		t.Errorf("the issue's own title did not come back: %s", res.Content)
	}
	// The conversation is a SEPARATE read. `gh-issue-show` answers "what is this issue", and a
	// comment list attached to it would grow without bound on a busy issue; the comments tool exists
	// for that. This asserted the reverse, which the tool never promised.
	t.Logf("3b. gh-issue-comments on #%d — the comment must be there", num)
	res, err = reg["gh-issue-comments"](ctx, map[string]any{"repo": repo, "issue_id": num}, session, tenant)
	if err != nil {
		t.Fatalf("gh-issue-comments: %v", err)
	}
	t.Logf("%s", indent(res.Content))
	if !strings.Contains(res.Content, "first comment from the probe") {
		t.Errorf("the comment did not come back: %s", res.Content)
	}

	t.Log("4. gh-issue-list — the new issue must appear, and pull requests must not")
	// The list is served from an index that lags a just-created issue by a few seconds, so this
	// polls rather than failing on the first attempt. The API itself is fine: gh-issue-show reads
	// the issue immediately, which is why the delay is worth naming instead of hiding.
	//
	// `kind: "issue"` is what excludes pull requests: GitHub returns both from the issues endpoint,
	// and the tool filters on that argument rather than guessing. Asking for everything and then
	// expecting no pull requests was testing an intention, not the tool.
	listed := false
	for attempt := 0; attempt < 10 && !listed; attempt++ {
		res, err = reg["gh-issue-list"](ctx, map[string]any{
			"repo": repo, "state": "all", "kind": "issue", "limit": 20,
		}, session, tenant)
		if err != nil {
			t.Fatalf("gh-issue-list: %v", err)
		}
		if strings.Contains(res.Content, marker) {
			listed = true
			break
		}
		time.Sleep(3 * time.Second)
	}
	t.Logf("%s", indent(res.Content))
	if !listed {
		t.Errorf("the issue just created never appeared in the list: %s", res.Content)
	}
	// With kind=issue, no pull request may appear. GitHub returns both from this endpoint, so this is
	// the assertion that the filter actually ran.
	if strings.Contains(res.Content, "flow: workspace end-to-end") {
		t.Errorf("a pull request leaked into a kind=issue list: %s", res.Content)
	}

	t.Logf("5. gh-issue-close on #%d", num)
	res, err = reg["gh-issue-close"](ctx, map[string]any{"repo": repo, "issue_id": num}, session, tenant)
	if err != nil {
		t.Fatalf("gh-issue-close: %v", err)
	}
	t.Logf("%s", indent(res.Content))

	// PR comments are covered by TestE2EPRComments. They used to be reached from here too, through
	// `gh-pr-list` and `gh-pr-comment` — names that no longer exist — and the code dereferenced the
	// nil result of the missing tool, so the test PANICKED rather than failing. Nothing caught it
	// because the whole suite skipped: the environment variable names did not match the ones CI set.
}
