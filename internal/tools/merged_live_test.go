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

// The merged tool set against real GitHub: one number space means one set of tools.
//
// GH_E2E=1 GH_TOKEN=... GH_E2E_REPO=owner/repo go test ./internal/tools -run TestE2EMergedItems
//
// Creates an issue, then asserts that the SAME tools read it as an issue and that a pull request
// would be read as a pull request — the merge is only correct if the tools report the kind rather
// than assuming it.
func TestE2EMergedItems(t *testing.T) {
	if os.Getenv("GH_E2E") != "1" {
		t.Skip("set GH_E2E=1 (and GH_TOKEN, GH_E2E_REPO)")
	}
	token, repo := os.Getenv("GH_TOKEN"), os.Getenv("GH_E2E_REPO")
	if token == "" || repo == "" {
		// A skip, not a failure: an unconfigured secret is a property of the deployment, not of the
		// code, and a red build for it says nothing about whether this works.
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
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	tenant, session := "alice", "merged"
	ctx = extension.WithGrants(ctx, []*easybus.Grant{
		{Key: PermMaintainer, Params: easybus.Struct(map[string]any{"repo": repo})},
	})

	marker := time.Now().Format("150405")
	t.Log("1. gh-issue-create")
	res, err := reg["gh-issue-create"](ctx, map[string]any{
		"repo": repo, "title": "merged tool probe " + marker, "body": "created by the merged test",
	}, session, tenant)
	if err != nil {
		t.Fatalf("gh-issue-create: %v", err)
	}
	n, _ := res.Data.(map[string]any)["issue"].(int)
	if n == 0 {
		t.Fatalf("no issue number: %+v", res.Data)
	}

	t.Logf("2. gh-issue-show %d — must report it as an ISSUE", n)
	res, err = reg["gh-issue-show"](ctx, map[string]any{"repo": repo, "issue_id": n}, session, tenant)
	if err != nil {
		t.Fatalf("gh-issue-show: %v", err)
	}
	t.Logf("%s", indent(res.Content))
	if !strings.Contains(res.Content, "(issue)") {
		t.Errorf("the reply does not identify it as an issue: %s", res.Content)
	}
	if v, _ := res.Data.(map[string]any)["is_pull_request"].(bool); v {
		t.Error("an issue was reported as a pull request")
	}

	t.Log("3. gh-issue-comment with a line on an ISSUE must be refused")
	_, err = reg["gh-issue-comment"](ctx, map[string]any{
		"repo": repo, "issue_id": n, "body": "x", "path": "README.md", "line": 1,
	}, session, tenant)
	if err == nil {
		t.Error("a line comment on an issue should be refused")
	} else {
		t.Logf("   refused as expected: %v", err)
	}

	t.Logf("4. gh-issue-comment (conversation) on %d", n)
	res, err = reg["gh-issue-comment"](ctx, map[string]any{
		"repo": repo, "issue_id": n, "body": "conversation probe",
	}, session, tenant)
	if err != nil {
		t.Fatalf("gh-issue-comment: %v", err)
	}
	t.Logf("%s", indent(res.Content))

	t.Log("5. gh-issue-list kind=issue — must contain it")
	res, err = reg["gh-issue-list"](ctx, map[string]any{
		"repo": repo, "state": "all", "kind": "issue", "limit": 20,
	}, session, tenant)
	if err != nil {
		t.Fatalf("gh-issue-list: %v", err)
	}
	t.Logf("%s", indent(res.Content))
	if strings.Contains(res.Content, "PR   ") {
		t.Errorf("kind=issue returned pull requests: %s", res.Content)
	}

	t.Logf("6. gh-issue-close %d — on its own issue, allowed for a developer", n)
	res, err = reg["gh-issue-close"](ctx, map[string]any{"repo": repo, "issue_id": n}, session, tenant)
	if err != nil {
		t.Fatalf("gh-issue-close: %v", err)
	}
	t.Logf("%s", indent(res.Content))
}
