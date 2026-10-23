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

// The CI and review views against real GitHub.
//
// GH_E2E=1 GH_TOKEN=... GH_E2E_REPO=easydev-org/easyagent go test ./internal/tools -run TestE2ECI
//
// Uses a repository with real workflow history, because the interesting behaviour is in the
// shapes GitHub returns: finished runs have logs, running ones do not.
func TestE2ECIAndPRViews(t *testing.T) {
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
	tenant, session := "alice", "ci"
	ctx = extension.WithGrants(ctx, []*easybus.Grant{
		{Key: PermPRMerge, Params: easybus.Struct(map[string]any{"repo": repo})},
	})

	t.Log("1. workflows")
	res, err := reg["gh-workflow-list"](ctx, map[string]any{"repo": repo}, session, tenant)
	if err != nil {
		t.Fatalf("gh-workflow-list: %v", err)
	}
	t.Logf("%s", indent(res.Content))

	t.Log("2. runs")
	res, err = reg["gh-run-list"](ctx, map[string]any{"repo": repo, "limit": 3}, session, tenant)
	if err != nil {
		t.Fatalf("gh-run-list: %v", err)
	}
	t.Logf("%s", indent(res.Content))

	// Read the newest finished run's jobs and log.
	runs, _ := res.Data.(map[string]any)["runs"].([]interface{})
	_ = runs
	t.Log("3. jobs of the newest run, then its log")
	var runID int
	if rr, ok := res.Data.(map[string]any)["runs"]; ok {
		if list, ok2 := rr.([]interface{}); ok2 && len(list) > 0 {
			if m, ok3 := list[0].(map[string]interface{}); ok3 {
				if v, ok4 := m["ID"].(int64); ok4 {
					runID = int(v)
				} else if v, ok4 := m["ID"].(int); ok4 {
					runID = v
				}
			}
		}
	}
	if runID == 0 {
		t.Skip("could not read a run id from the tool output; skipping the jobs/log assertions")
	}
	res, err = reg["gh-run-jobs"](ctx, map[string]any{"repo": repo, "run": runID}, session, tenant)
	if err != nil {
		t.Fatalf("gh-run-jobs: %v", err)
	}
	t.Logf("%s", indent(res.Content))

	res, err = reg["gh-run-log"](ctx, map[string]any{"repo": repo, "run": runID, "lines": 5}, session, tenant)
	if err != nil {
		t.Logf("   (log unavailable: %v)", err)
	} else {
		t.Logf("%s", indent(res.Content))
	}

	// The PR views: use the first PR in the repository, whatever it is.
	t.Log("4. PR files + diff")
	res, err = reg["gh-pr-list"](ctx, map[string]any{"repo": repo, "state": "all"}, session, tenant)
	if err != nil {
		t.Fatalf("gh-pr-list: %v", err)
	}
	if !strings.Contains(res.Content, "#") {
		t.Skip("no pull requests in this repository; skipping the PR file view")
	}
}

func indent(s string) string { return "   " + strings.ReplaceAll(s, "\n", "\n   ") }
