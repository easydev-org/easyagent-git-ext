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
		t.Fatal("GH_TOKEN and GH_E2E_REPO are required")
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
		{Key: PermIssueCreate, Params: easybus.Struct(map[string]any{"repo": repo})},
		{Key: PermIssueComment, Params: easybus.Struct(map[string]any{"repo": repo})},
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
		"repo": repo, "number": num, "body": "first comment from the probe",
	}, session, tenant)
	if err != nil {
		t.Fatalf("gh-issue-comment: %v", err)
	}
	t.Logf("%s", indent(res.Content))

	t.Logf("3. gh-issue-show on #%d — the comment must be there", num)
	res, err = reg["gh-issue-show"](ctx, map[string]any{"repo": repo, "number": num}, session, tenant)
	if err != nil {
		t.Fatalf("gh-issue-show: %v", err)
	}
	t.Logf("%s", indent(res.Content))
	if !strings.Contains(res.Content, "first comment from the probe") {
		t.Errorf("the comment did not come back: %s", res.Content)
	}

	t.Log("4. gh-issue-list — the new issue must appear, and pull requests must not")
	// The list is served from an index that lags a just-created issue by a few seconds, so this
	// polls rather than failing on the first attempt. The API itself is fine: gh-issue-show reads
	// the issue immediately, which is why the delay is worth naming instead of hiding.
	listed := false
	for attempt := 0; attempt < 10 && !listed; attempt++ {
		res, err = reg["gh-issue-list"](ctx, map[string]any{"repo": repo, "state": "all", "limit": 20}, session, tenant)
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
	// Pull requests must NOT be mixed in: GitHub returns both from this endpoint.
	if strings.Contains(res.Content, "flow: workspace end-to-end") {
		t.Errorf("a pull request leaked into the issue list: %s", res.Content)
	}

	t.Logf("5. gh-issue-close on #%d", num)
	res, err = reg["gh-issue-close"](ctx, map[string]any{"repo": repo, "number": num}, session, tenant)
	if err != nil {
		t.Fatalf("gh-issue-close: %v", err)
	}
	t.Logf("%s", indent(res.Content))

	// 6. A PR comment: only meaningful when the repo has an open PR, so it is reported rather
	// than failed when there is none.
	t.Log("6. gh-pr-comment (if an open PR exists)")
	res, err = reg["gh-pr-list"](ctx, map[string]any{"repo": repo, "state": "open"}, session, tenant)
	if err == nil {
		if prs, ok := res.Data.(map[string]any)["pulls"].([]interface{}); ok && len(prs) > 0 {
			if m, ok := prs[0].(map[string]interface{}); ok {
				if pn, ok := m["Number"].(int); ok {
					res, cerr := reg["gh-pr-comment"](ctx, map[string]any{
						"repo": repo, "number": pn, "body": "probe comment on the PR conversation",
					}, session, tenant)
					if cerr != nil {
						t.Fatalf("gh-pr-comment: %v", cerr)
					}
					t.Logf("%s", indent(res.Content))

					res, cerr = reg["gh-pr-comments"](ctx, map[string]any{"repo": repo, "number": pn}, session, tenant)
					if cerr != nil {
						t.Fatalf("gh-pr-comments: %v", cerr)
					}
					t.Logf("%s", indent(res.Content))
					if !strings.Contains(res.Content, "probe comment on the PR conversation") {
						t.Errorf("the PR comment did not come back: %s", res.Content)
					}
				}
			}
		} else {
			t.Log("   (no open pull request in this repo, skipping the PR comment)")
		}
	}
}
