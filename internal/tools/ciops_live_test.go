package tools

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	easybus "github.com/easydev-org/easybus-sdk"
	"github.com/easydev-org/easybus-sdk/extension"

	"github.com/easydev-org/easyagent-github-ext/internal/ghclient"
)

// The CI operation tools against real GitHub.
//
// GH_E2E=1 GH_TOKEN=... GH_E2E_REPO=owner/repo GH_E2E_WORKFLOW=<file.yml> go test ./internal/tools -run TestE2ECIOps
//
// Exercises the whole loop: dispatch a run, watch it, list its artifacts, download one into
// object storage, then rerun and cancel. Those are the tools that can CHANGE CI, so they are
// worth exercising end to end rather than only at the handler boundary.
func TestE2ECIOps(t *testing.T) {
	if os.Getenv("GH_E2E") != "1" {
		t.Skip("set GH_E2E=1 (and GH_TOKEN, GH_E2E_REPO, GH_E2E_WORKFLOW)")
	}
	token, repo := os.Getenv("GH_TOKEN"), os.Getenv("GH_E2E_REPO")
	workflow := os.Getenv("GH_E2E_WORKFLOW")
	if token == "" || repo == "" || workflow == "" {
		// SKIPPED, not failed. CI sets the first two but cannot guarantee a repository with a
		// workflow_dispatch-able workflow: that is a property of the target, not of this deployment,
		// and requiring it would make the suite fail for a reason nobody can act on.
		t.Skip("set GH_E2E_WORKFLOW to a workflow_dispatch-enabled workflow in GH_E2E_REPO to run this")
	}

	// A stub object store, so the test proves the artifact reached storage without needing the
	// real bucket.
	var stored struct {
		name string
		size int
	}
	reg := Registry(Deps{
		GetConfig: func(name, session, tenant string) any {
			if name == "token" {
				return token
			}
			return nil
		},
		Locale: func(string, string) string { return "en" },
		PutObject: func(ctx context.Context, tenant, name string, data []byte) error {
			stored.name, stored.size = name, len(data)
			return nil
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	tenant, session := "alice", "ciops"
	ctx = extension.WithGrants(ctx, []*easybus.Grant{
		{Key: PermMaintainer, Params: easybus.Struct(map[string]any{"repo": repo})},
		{Key: PermMaintainer, Params: easybus.Struct(map[string]any{"repo": repo})},
		{Key: PermMaintainer, Params: easybus.Struct(map[string]any{"repo": repo})},
	})

	t.Log("1. gh-run-dispatch")
	res, err := reg["gh-run-dispatch"](ctx, map[string]any{
		"repo": repo, "workflow": workflow,
	}, session, tenant)
	if err != nil {
		t.Fatalf("gh-run-dispatch: %v", err)
	}
	t.Logf("%s", indent(res.Content))
	runID, _ := res.Data.(map[string]any)["run"].(int64)
	if runID == 0 {
		if v, ok := res.Data.(map[string]any)["run"].(int); ok {
			runID = int64(v)
		}
	}
	if runID == 0 {
		t.Fatal("dispatch returned no run id")
	}

	t.Logf("2. gh-run-jobs on the new run %d", runID)
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		res, jerr := reg["gh-run-jobs"](ctx, map[string]any{"repo": repo, "run": int(runID)}, session, tenant)
		if jerr == nil && strings.Contains(res.Content, "success") {
			t.Logf("%s", indent(res.Content))
			break
		}
		time.Sleep(5 * time.Second)
	}

	t.Logf("3. gh-artifacts on run %d", runID)
	res, err = reg["gh-artifacts"](ctx, map[string]any{"repo": repo, "run": int(runID)}, session, tenant)
	if err != nil {
		t.Fatalf("gh-artifacts: %v", err)
	}
	t.Logf("%s", indent(res.Content))

	// Download the first artifact if the run produced one.
	if items, ok := res.Data.(map[string]any)["artifacts"].([]ghclient.Artifact); ok && len(items) > 0 {
		{
			if id := items[0].ID; id > 0 {
				t.Logf("4. gh-artifact-download artifact %d", id)
				res, derr := reg["gh-artifact-download"](ctx, map[string]any{
					"repo": repo, "artifact": int(id), "name": "probe",
				}, session, tenant)
				if derr != nil {
					t.Fatalf("gh-artifact-download: %v", derr)
				}
				t.Logf("%s", indent(res.Content))
				if stored.size == 0 {
					t.Error("the artifact reached no object storage")
				} else {
					t.Logf("   stored %s (%d bytes)", stored.name, stored.size)
				}
			}
		}
	} else {
		t.Log("   (this run produced no artifacts, skipping the download)")
	}

	t.Logf("5. gh-run-rerun on %d (failed_only)", runID)
	res, err = reg["gh-run-rerun"](ctx, map[string]any{"repo": repo, "run": int(runID)}, session, tenant)
	if err != nil {
		// A run with no failures cannot be rerun this way; that is a legitimate refusal.
		t.Logf("   refused (expected when nothing failed): %v", err)
	} else {
		t.Logf("%s", indent(res.Content))
	}

	t.Logf("6. gh-run-cancel on %d", runID)
	res, err = reg["gh-run-cancel"](ctx, map[string]any{"repo": repo, "run": int(runID)}, session, tenant)
	if err != nil {
		t.Logf("   refused (expected once the run already finished): %v", err)
	} else {
		t.Logf("%s", indent(res.Content))
	}
}
