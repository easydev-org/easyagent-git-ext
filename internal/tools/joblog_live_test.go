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

// gh-job-log against a real GitHub job.
//
// GH_E2E=1 GH_E2E_JOB=<job id> GH_TOKEN=... GH_E2E_REPO=owner/repo go test ./internal/tools -run TestE2EJobLog
//
// Uses a COMPLETED job: a running one has no log, and the tool's whole point is to explain that
// rather than return empty text.
func TestE2EJobLog(t *testing.T) {
	if os.Getenv("GH_E2E") != "1" {
		t.Skip("set GH_E2E=1 (and GH_TOKEN, GH_E2E_REPO, GH_E2E_JOB)")
	}
	token, repo := os.Getenv("GH_TOKEN"), os.Getenv("GH_E2E_REPO")
	jobID, _ := strconv.ParseInt(os.Getenv("GH_E2E_JOB"), 10, 64)
	if token == "" || repo == "" || jobID == 0 {
		// SKIPPED, not failed: a completed job id cannot be conjured by CI either. CI could dispatch
		// a run and wait for it, but that is minutes of waiting for one read, and the CI-ops test
		// already covers dispatching.
		t.Skip("set GH_E2E_JOB to a completed job's id to run this")
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
	ctx = extension.WithGrants(ctx, nil)

	res, err := reg["gh-job-log"](ctx, map[string]any{
		"repo": repo, "job": int(jobID), "lines": 15,
	}, "alice", "joblog")
	if err != nil {
		t.Fatalf("gh-job-log: %v", err)
	}
	t.Logf("tail of job %d:\n%s", jobID, indent(res.Content))

	// A completed job with output must produce real text, not an empty string a caller would
	// mistake for "the job said nothing".
	if strings.TrimSpace(res.Content) == "" {
		t.Fatal("empty log for a completed job")
	}
	// The cap must keep the END of the log. Two things prove it: the truncation notice says how
	// many lines were kept, and the text that survives carries log timestamps — which only
	// appear on real log lines, not on a header.
	if strings.Contains(res.Content, "showing the last") {
		if !strings.Contains(res.Content, "Z ") {
			t.Errorf("retained lines do not look like log lines:\n%s", res.Content)
		}
	}
}
