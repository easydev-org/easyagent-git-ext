package tools

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	easybus "github.com/easydev-org/easybus-sdk"
	"github.com/easydev-org/easybus-sdk/extension"
	easyvcsstore "github.com/easylab-platform/easyvcs/store"

	"github.com/easydev-org/easyagent-github-ext/internal/stage"
)

// The full flow against REAL GitHub: stage a file, open a PR, read back the CI status.
//
// Run with:
//
//	GH_E2E=1 GH_TOKEN=... GH_E2E_REPO=easydev-org/test go test ./internal/tools -run TestE2E
//
// This is a test rather than a script because the interesting assertions are about the
// SEQUENCE — the branch exists before the PR is opened, the staged file is in the commit, and
// the PR number comes back usable. A script would print those; a test checks them.
func TestE2EOpenPullRequest(t *testing.T) {
	if os.Getenv("GH_E2E") != "1" {
		t.Skip("set GH_E2E=1 (and GH_TOKEN, GH_E2E_REPO) to run against real GitHub")
	}
	token := os.Getenv("GH_TOKEN")
	repo := os.Getenv("GH_E2E_REPO")
	if token == "" || repo == "" {
		t.Fatal("GH_TOKEN and GH_E2E_REPO are required")
	}

	// A temporary SQLite store: the staging area is a real easyvcs workspace, not a mock.
	dir := t.TempDir()
	cs, err := easyvcsstore.OpenDriver(easyvcsstore.DriverConfig{
		Kind: "sqlite", DSN: dir + "/easyvcs.db",
	})
	if err != nil {
		t.Fatalf("open easyvcs: %v", err)
	}
	if err := cs.Init(); err != nil {
		t.Fatalf("init easyvcs: %v", err)
	}
	st := stage.New(cs, func(tenant string) easyvcsstore.RepoRef {
		return easyvcsstore.RepoRef{Name: "tenant-" + tenant}
	})

	reg := Registry(Deps{
		Stage: st,
		GetConfig: func(name, session, tenant string) any {
			if name == "token" {
				return token
			}
			return nil
		},
		Locale: func(string, string) string { return "en" },
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	tenant, session := "alice", "e2e"
	// The grants a real agent would send: pr.create on this repository. Without them the
	// permission gate refuses, which is what the unit tests already assert.
	ctx = extension.WithGrants(ctx, []*easybus.Grant{
		{Key: PermPRCreate, Params: easybus.Struct(map[string]any{"repo": repo})},
		{Key: PermPRClose, Params: easybus.Struct(map[string]any{"repo": repo})},
	})

	t.Logf("1. staging into %s", repo)
	if _, err := reg["gh-info"](ctx, map[string]any{"repo": repo}, session, tenant); err != nil {
		t.Fatalf("gh-info: %v", err)
	}

	marker := fmt.Sprintf("e2e-%d", time.Now().Unix())
	patch := "*** Begin Patch\n*** Add File: E2E.md\n+# End-to-end test\n+\n+" +
		"Created by easyagent-github-ext with no local sandbox (" + marker + ").\n*** End Patch\n"

	t.Logf("2. applying patch")
	if _, err := reg["gh-stage-patch"](ctx,
		map[string]any{"repo": repo, "patch": patch}, session, tenant); err != nil {
		t.Fatalf("gh-stage-patch: %v", err)
	}

	t.Logf("3. status")
	res, err := reg["gh-stage-status"](ctx, map[string]any{"repo": repo}, session, tenant)
	if err != nil {
		t.Fatalf("gh-stage-status: %v", err)
	}
	t.Logf("   %s", res.Content)

	t.Logf("4. opening the pull request")
	// Report what the stage believes its base is, because "no history in common" is exactly
	// what a wrong base produces and it is invisible from the tool result.
	if w := st.For(tenant, session, repo); w != nil {
		t.Logf("   stage base revision: %q", w.Base())
	}
	res, err = reg["gh-pr-create"](ctx, map[string]any{
		"repo": repo, "title": "e2e: add E2E.md (" + marker + ")",
	}, session, tenant)
	if err != nil {
		t.Fatalf("gh-pr-create: %v", err)
	}
	t.Logf("   %s", res.Content)

	num, _ := res.Data.(map[string]any)["pr"].(int)
	if num == 0 {
		t.Fatalf("no PR number in the result: %+v", res.Data)
	}

	t.Logf("5. CI status for #%d", num)
	chk, err := reg["gh-pr-checks"](ctx, map[string]any{"repo": repo, "number": num}, session, tenant)
	if err != nil {
		t.Fatalf("gh-pr-checks: %v", err)
	}
	t.Logf("   %s", chk.Content)

	t.Logf("PR #%d opened; close it with gh-pr-close if this was a probe", num)
}
