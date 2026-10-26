package ghclient

import (
	"context"
	"fmt"
)

// RunActor returns the login that TRIGGERED a workflow run.
//
// Deliberately not the commit author and not the workflow file's owner: GitHub answers "who started
// this run" with triggering_actor, and that is the only field that makes an ownership check mean
// what a reader expects. A maintainer re-running a contributor's build is the case that separates
// the two — the commit author is still the contributor.
func (c *Client) RunActor(ctx context.Context, runID int64) (string, error) {
	run, resp, err := c.gh.Actions.GetWorkflowRunByID(ctx, c.Owner, c.Repo, runID)
	c.note(resp)
	if err != nil {
		return "", wrap(err)
	}
	if a := run.GetTriggeringActor(); a != nil && a.GetLogin() != "" {
		return a.GetLogin(), nil
	}
	// Older runs, and events with no actor (a schedule), have no triggering_actor. Falling back to
	// the run's own actor field is better than reporting an empty owner, which would look like
	// "nobody" and let an ownership check pass by accident.
	if a := run.GetActor(); a != nil && a.GetLogin() != "" {
		return a.GetLogin(), nil
	}
	return "", fmt.Errorf("GitHub reported no triggering actor for run %d", runID)
}
