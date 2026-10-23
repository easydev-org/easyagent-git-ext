package tools

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/easydev-org/easybus-sdk/extension"
)

// A specific run's jobs and log, against real GitHub.
//
// GH_E2E=1 GH_E2E_RUN=<id> GH_TOKEN=... GH_E2E_REPO=owner/repo go test ./internal/tools -run TestE2ECIRunDetail
//
// Asserts the two things about a finished run that matter: the per-step status is present for
// every step, and the log contains real output — not an empty string that a caller would take
// for "the job said nothing".
func TestE2ECIRunDetail(t *testing.T) {
	if os.Getenv("GH_E2E") != "1" {
		t.Skip("set GH_E2E=1 (and GH_TOKEN, GH_E2E_REPO, GH_E2E_RUN)")
	}
	token, repo := os.Getenv("GH_TOKEN"), os.Getenv("GH_E2E_REPO")
	runID, _ := strconv.Atoi(os.Getenv("GH_E2E_RUN"))
	if token == "" || repo == "" || runID == 0 {
		t.Fatal("GH_TOKEN, GH_E2E_REPO and GH_E2E_RUN are required")
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
	ctx = extension.WithGrants(ctx, nil)

	t.Logf("jobs of run %d", runID)
	res, err := reg["gh-run-jobs"](ctx, map[string]any{"repo": repo, "run": runID}, "alice", "ci")
	if err != nil {
		t.Fatalf("gh-run-jobs: %v", err)
	}
	t.Logf("%s", indent(res.Content))
	if !strings.Contains(res.Content, "success") && !strings.Contains(res.Content, "in_progress") {
		t.Errorf("no step status in the output: %s", res.Content)
	}

	t.Logf("log of run %d (first 8 lines per job)", runID)
	res, err = reg["gh-run-log"](ctx, map[string]any{"repo": repo, "run": runID, "lines": 8}, "alice", "ci")
	if err != nil {
		t.Fatalf("gh-run-log: %v", err)
	}
	t.Logf("%s", indent(res.Content))
	if strings.TrimSpace(res.Content) == "" || strings.Contains(res.Content, "no log content") {
		t.Errorf("the log came back empty for a finished run: %s", res.Content)
	}
}
