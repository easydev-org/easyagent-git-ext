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

// Pull request comments, both shapes, against a real pull request.
//
// GH_E2E=1 GH_TOKEN=... GH_E2E_REPO=owner/repo GH_E2E_PR=<number> go test ./internal/tools -run TestE2EPRComments
//
// A conversation comment and a line comment are one API resource but two different things to a
// reader, and a tool that only ever produced one of them would look like it worked.
func TestE2EPRComments(t *testing.T) {
	if os.Getenv("GH_E2E") != "1" {
		t.Skip("set GH_E2E=1 (and GH_TOKEN, GH_E2E_REPO, GH_E2E_PR)")
	}
	token, repo := os.Getenv("GH_TOKEN"), os.Getenv("GH_E2E_REPO")
	prNum := atoiSafe(os.Getenv("GH_E2E_PR"))
	if token == "" || repo == "" || prNum == 0 {
		t.Fatal("GH_TOKEN, GH_E2E_REPO and GH_E2E_PR are required")
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
	ctx = extension.WithGrants(ctx, []*easybus.Grant{
		{Key: PermIssueComment, Params: easybus.Struct(map[string]any{"repo": repo})},
	})
	session, tenant := "prcomment", "alice"

	t.Log("1. conversation comment")
	res, err := reg["gh-pr-comment"](ctx, map[string]any{
		"repo": repo, "number": prNum, "body": "conversation probe CONVO",
	}, session, tenant)
	if err != nil {
		t.Fatalf("gh-pr-comment (conversation): %v", err)
	}
	t.Logf("%s", indent(res.Content))

	t.Log("2. line comment")
	res, err = reg["gh-pr-comment"](ctx, map[string]any{
		"repo": repo, "number": prNum, "body": "line probe INLINE",
		"path": "COMMENT.md", "line": 2,
	}, session, tenant)
	if err != nil {
		t.Fatalf("gh-pr-comment (line): %v", err)
	}
	t.Logf("%s", indent(res.Content))

	t.Log("3. gh-pr-comments must show BOTH")
	res, err = reg["gh-pr-comments"](ctx, map[string]any{"repo": repo, "number": prNum}, session, tenant)
	if err != nil {
		t.Fatalf("gh-pr-comments: %v", err)
	}
	t.Logf("%s", indent(res.Content))
	if !strings.Contains(res.Content, "CONVO") {
		t.Error("the conversation comment is missing from gh-pr-comments")
	}
	if !strings.Contains(res.Content, "INLINE") {
		t.Error("the line comment is missing from gh-pr-comments")
	}
	if !strings.Contains(res.Content, "COMMENT.md:2") {
		t.Error("the line comment does not report its location")
	}

	t.Log("4. half a location must be refused")
	if _, err := reg["gh-pr-comment"](ctx, map[string]any{
		"repo": repo, "number": prNum, "body": "x", "path": "COMMENT.md",
	}, session, tenant); err == nil {
		t.Error("a path without a line should be refused")
	}
}

// atoiSafe parses without importing strconv at the top for one call.
func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}
