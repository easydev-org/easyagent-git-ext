package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	easybus "github.com/easydev-org/easybus-sdk"
	"github.com/easydev-org/easybus-sdk/extension"
	easyvcsstore "github.com/easylab-platform/easyvcs/store"

	"github.com/easydev-org/easyagent-github-ext/internal/stage"
)

// The full review loop against real GitHub: stage several changes, open a PR, then read back
// the file list and the diff.
//
// This is the check that the PR is REVIEWABLE, which is the whole point of a pull request. It
// creates a multi-file change so the list, the per-file counts and the unified diff all have
// something to report.
func TestE2EPRFilesAndDiff(t *testing.T) {
	if os.Getenv("GH_E2E") != "1" {
		t.Skip("set GH_E2E=1 (and GH_TOKEN, GH_E2E_REPO)")
	}
	token, repo := os.Getenv("GH_TOKEN"), os.Getenv("GH_E2E_REPO")
	if token == "" || repo == "" {
		t.Fatal("GH_TOKEN and GH_E2E_REPO are required")
	}

	dir := t.TempDir()
	cs, err := easyvcsstore.OpenDriver(easyvcsstore.DriverConfig{Kind: "sqlite", DSN: dir + "/vcs.db"})
	if err != nil {
		t.Fatalf("easyvcs: %v", err)
	}
	if err := cs.Init(); err != nil {
		t.Fatalf("easyvcs init: %v", err)
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
	tenant, session := "alice", "prfiles"
	ctx = extension.WithGrants(ctx, []*easybus.Grant{
		{Key: PermPRCreate, Params: easybus.Struct(map[string]any{"repo": repo})},
		{Key: PermPRClose, Params: easybus.Struct(map[string]any{"repo": repo})},
	})

	marker := fmt.Sprintf("%d", time.Now().Unix())
	// Two added files and one update to E2E.md, which PR #1 created.
	// The Update hunk uses the file's FIRST line as context with no anchor: the anchor names a
	// line the hunk sits BELOW, and here there is nothing above it.
	patch := "*** Begin Patch\n" +
		"*** Add File: review/a.txt\n+line one\n+line two\n+line three\n" +
		"*** Add File: review/b.txt\n+only one line\n" +
		"*** Update File: E2E.md\n@@\n # End-to-end test\n+\n+Reviewed in " + marker + ".\n" +
		"*** End Patch\n"

	t.Log("1. staging three changes")
	if _, err := reg["gh-stage-patch"](ctx,
		map[string]any{"repo": repo, "patch": patch}, session, tenant); err != nil {
		t.Fatalf("gh-stage-patch: %v", err)
	}
	res, err := reg["gh-stage-status"](ctx, map[string]any{"repo": repo}, session, tenant)
	if err != nil {
		t.Fatalf("gh-stage-status: %v", err)
	}
	t.Logf("%s", indent(res.Content))

	t.Log("2. opening the PR")
	res, err = reg["gh-pr-create"](ctx, map[string]any{
		"repo": repo, "title": "review: multi-file change " + marker,
	}, session, tenant)
	if err != nil {
		t.Fatalf("gh-pr-create: %v", err)
	}
	t.Logf("%s", indent(res.Content))
	num, _ := res.Data.(map[string]any)["pr"].(int)
	if num == 0 {
		t.Fatalf("no PR number: %+v", res.Data)
	}

	t.Logf("3. files of #%d", num)
	res, err = reg["gh-pr-files"](ctx, map[string]any{"repo": repo, "number": num}, session, tenant)
	if err != nil {
		t.Fatalf("gh-pr-files: %v", err)
	}
	t.Logf("%s", indent(res.Content))
	if got, _ := res.Data.(map[string]any)["count"].(int); got != 3 {
		t.Errorf("expected 3 changed files, got %d: %s", got, res.Content)
	}

	t.Logf("4. diff of #%d", num)
	res, err = reg["gh-pr-diff"](ctx, map[string]any{"repo": repo, "number": num}, session, tenant)
	if err != nil {
		t.Fatalf("gh-pr-diff: %v", err)
	}
	t.Logf("%s", indent(res.Content))
	if !strings.Contains(res.Content, "review/a.txt") || !strings.Contains(res.Content, "+line two") {
		t.Errorf("the diff does not contain the staged content: %s", res.Content)
	}

	t.Logf("PR #%d left open for inspection", num)
}
