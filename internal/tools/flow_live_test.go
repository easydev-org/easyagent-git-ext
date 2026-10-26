package tools

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	easybus "github.com/easydev-org/easybus-sdk"
	"github.com/easydev-org/easybus-sdk/extension"

	"github.com/easydev-org/easyagent-github-ext/internal/workspace"
)

// The whole local-git flow against real GitHub:
//
//	clone → patch → diff → commit → (again) → push branch → PR
//
// This is the shape a session actually works in, and every step is a separate tool because each
// one can fail for its own reason. Running it end to end is the only way to know the pieces fit.
//
// GH_E2E=1 GH_TOKEN=... GH_E2E_REPO=easydev-org/test go test ./internal/tools -run TestE2EWorkspaceFlow
func TestE2EWorkspaceFlow(t *testing.T) {
	if os.Getenv("GH_E2E") != "1" {
		t.Skip("set GH_E2E=1 (and GH_TOKEN, GH_E2E_REPO)")
	}
	token, repo := os.Getenv("GH_TOKEN"), os.Getenv("GH_E2E_REPO")
	if token == "" || repo == "" {
		t.Fatal("GH_TOKEN and GH_E2E_REPO are required")
	}

	mgr, err := workspace.NewManager(t.TempDir(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	reg := Registry(Deps{
		Workspaces: mgr,
		GetConfig: func(name, session, tenant string) any {
			if name == "token" {
				return token
			}
			return nil
		},
		Locale: func(string, string) string { return "en" },
	})
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	tenant, session := "alice", "flow"
	ctx = extension.WithGrants(ctx, []*easybus.Grant{
		{Key: PermMaintainer, Params: easybus.Struct(map[string]any{"repo": repo})},
	})

	t.Log("1. git-ws-clone")
	res, err := reg["git-ws-clone"](ctx, map[string]any{"repo": repo}, session, tenant)
	if err != nil {
		t.Fatalf("git-ws-clone: %v", err)
	}
	wsID, _ := res.Data.(map[string]any)["workspace"].(string)
	if wsID == "" {
		t.Fatalf("no workspace id: %+v", res.Data)
	}
	t.Logf("%s", indent(res.Content))

	t.Log("2. git-ws-log (real history proves this is a real checkout)")
	res, err = reg["git-ws-log"](ctx, map[string]any{"workspace": wsID, "limit": 3}, session, tenant)
	if err != nil {
		t.Fatalf("git-ws-log: %v", err)
	}
	t.Logf("%s", indent(res.Content))
	if !strings.Contains(res.Content, "commits") {
		t.Errorf("git-ws-log looks empty: %s", res.Content)
	}

	t.Log("3. git-ws-patch")
	marker := "flow-" + time.Now().Format("150405")
	patch := "*** Begin Patch\n*** Add File: FLOW.md\n+# Workspace flow\n+\n+" + marker + "\n*** End Patch\n"
	res, err = reg["git-ws-patch"](ctx, map[string]any{"workspace": wsID, "patch": patch}, session, tenant)
	if err != nil {
		t.Fatalf("git-ws-patch: %v", err)
	}
	t.Logf("%s", indent(res.Content))

	t.Log("4. git-ws-status + git-ws-diff")
	res, _ = reg["git-ws-status"](ctx, map[string]any{"workspace": wsID}, session, tenant)
	t.Logf("%s", indent(res.Content))
	res, err = reg["git-ws-diff"](ctx, map[string]any{"workspace": wsID}, session, tenant)
	if err != nil {
		t.Fatalf("git-ws-diff: %v", err)
	}
	t.Logf("%s", indent(res.Content))
	if !strings.Contains(res.Content, "+"+marker) && !strings.Contains(res.Content, marker) {
		t.Errorf("the diff does not show the new content: %s", res.Content)
	}

	t.Log("5. git-ws-commit (twice — this is the point of separating it)")
	if _, err = reg["git-ws-commit"](ctx, map[string]any{"workspace": wsID, "message": "flow: add FLOW.md"}, session, tenant); err != nil {
		t.Fatalf("git-ws-commit: %v", err)
	}
	// A second, unrelated change committed separately.
	// No anchor: the hunk sits at the top of the file, so there is no line above it to name.
	patch2 := "*** Begin Patch\n*** Update File: FLOW.md\n@@\n # Workspace flow\n+\n+second commit\n*** End Patch\n"
	if _, err = reg["git-ws-patch"](ctx, map[string]any{"workspace": wsID, "patch": patch2}, session, tenant); err != nil {
		t.Fatalf("git-ws-patch 2: %v", err)
	}
	if _, err = reg["git-ws-commit"](ctx, map[string]any{"workspace": wsID, "message": "flow: second commit"}, session, tenant); err != nil {
		t.Fatalf("git-ws-commit 2: %v", err)
	}
	res, _ = reg["git-ws-log"](ctx, map[string]any{"workspace": wsID, "limit": 3}, session, tenant)
	t.Logf("after two commits:\n%s", indent(res.Content))
	if !strings.Contains(res.Content, "second commit") {
		t.Errorf("the second commit is missing from the log: %s", res.Content)
	}

	t.Log("6. gh-pr-create (publishes the committed branch)")
	res, err = reg["gh-pr-create"](ctx, map[string]any{
		"workspace": wsID, "repo": repo, "title": "flow: workspace end-to-end " + marker,
	}, session, tenant)
	if err != nil {
		t.Fatalf("gh-pr-create: %v", err)
	}
	t.Logf("%s", indent(res.Content))
	num, _ := res.Data.(map[string]any)["pr"].(int)
	if num == 0 {
		t.Fatalf("no PR number: %+v", res.Data)
	}

	t.Logf("opened PR #%d", num)
}

// indent prefixes every line, so a multi-line tool result reads as one block in test output.
func indent(s string) string {
	return "   " + strings.ReplaceAll(s, "\n", "\n   ")
}
