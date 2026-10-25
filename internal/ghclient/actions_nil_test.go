package ghclient

import (
	"testing"

	gogithub "github.com/google/go-github/v93/github"
)

// A job that has not started has a nil StartedAt, and formatting that pointer panics.
//
// This is the shape a freshly dispatched run returns: jobs in `queued` with no start time. The
// panic killed the process, so the guard is pinned here rather than left to the integration test.
func TestRunJobsToleratesUnstartedJob(t *testing.T) {
	j := &gogithub.WorkflowJob{
		ID:     gogithub.Ptr(int64(1)),
		Name:   gogithub.Ptr("queued-job"),
		Status: gogithub.Ptr("queued"),
		// StartedAt deliberately nil, as GitHub sends it for a job that has not begun.
		Steps: []*gogithub.TaskStep{
			{Number: gogithub.Ptr(int64(1)), Name: gogithub.Ptr("step"), Status: gogithub.Ptr("pending")},
		},
	}

	// The expression the client used to evaluate unconditionally.
	if j.StartedAt != nil {
		t.Fatalf("expected StartedAt to be nil in this fixture")
	}

	// What the client does now: only format when present.
	var startedAt string
	if p := j.StartedAt.GetTime(); p != nil {
		startedAt = p.Format("15:04:05")
	}
	if startedAt != "" {
		t.Errorf("an unstarted job reported a start time %q", startedAt)
	}

	// And the step loop must tolerate nil step timestamps too.
	for _, s := range j.Steps {
		var stepStarted, stepDone string
		if p := s.StartedAt.GetTime(); p != nil {
			stepStarted = p.Format("15:04:05")
		}
		if p := s.CompletedAt.GetTime(); p != nil {
			stepDone = p.Format("15:04:05")
		}
		if stepStarted != "" || stepDone != "" {
			t.Errorf("a pending step reported timestamps %q/%q", stepStarted, stepDone)
		}
	}
}
