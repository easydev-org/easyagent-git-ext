package ghclient

import (
	"context"
	"fmt"

	gogithub "github.com/google/go-github/v93/github"
)

// CI results for a pull request.
//
// Two APIs both matter and are easy to confuse:
//
//   - CHECK RUNS (newer, what GitHub Actions writes) — per job, with a conclusion;
//   - COMMIT STATUSES (older, what third-party CI writes).
//
// A repository may use either, so "is CI green" has to consider both. Looking at only one would
// report a passing build while a status context is failing.

// CheckRun is one job of a CI run.
type CheckRun struct {
	Name       string
	Status     string // queued | in_progress | completed
	Conclusion string // success | failure | neutral | cancelled | skipped | timed_out | action_required
	DetailsURL string
}

// Status is one commit-status context.
type Status struct {
	Context string
	State   string // error | failure | pending | success
	Target  string
}

// Checks summarizes CI for a commit.
type Checks struct {
	Ref      string
	Runs     []CheckRun
	Statuses []Status
	// HeadSHA is the commit these results belong to, so a caller can tell whether CI has even
	// started for the commit it just pushed.
	HeadSHA string
}

// State reduces everything to one word a caller can branch on:
//
//	"success"  everything finished and passed
//	"failure"  something definitively failed
//	"pending"  something has not finished
//	"none"     no CI is configured / has not started
//
// A single failure dominates a pending result: once something has failed, waiting for the rest
// tells the caller nothing they need.
func (c Checks) State() string {
	if len(c.Runs) == 0 && len(c.Statuses) == 0 {
		return "none"
	}
	pending := false
	for _, r := range c.Runs {
		if r.Status != "completed" {
			pending = true
			continue
		}
		switch r.Conclusion {
		case "failure", "timed_out", "cancelled", "action_required":
			return "failure"
		}
	}
	for _, s := range c.Statuses {
		switch s.State {
		case "failure", "error":
			return "failure"
		case "pending":
			pending = true
		}
	}
	if pending {
		return "pending"
	}
	return "success"
}

// PRChecks collects CI results for a pull request's HEAD commit.
//
// Reads the PR first to find the head SHA: asking by branch NAME is ambiguous while a push is
// in flight, and by the time a caller asks the branch may already point elsewhere.
func (c *Client) PRChecks(ctx context.Context, number int) (Checks, error) {
	pr, resp, err := c.gh.PullRequests.Get(ctx, c.Owner, c.Repo, number)
	c.note(resp)
	if err != nil {
		return Checks{}, wrap(err)
	}
	sha := pr.GetHead().GetSHA()
	if sha == "" {
		return Checks{}, fmt.Errorf("pull request %d has no head sha", number)
	}

	var runs []CheckRun
	// Paged: a matrix build easily exceeds one page, and a missing job could be the failing one.
	err = paginate(func(page int) (*gogithub.Response, error) {
		res, resp, err := c.gh.Checks.ListCheckRunsForRef(ctx, c.Owner, c.Repo, sha,
			&gogithub.ListCheckRunsOptions{ListOptions: gogithub.ListOptions{Page: page, PerPage: 100}})
		c.note(resp)
		if err != nil {
			return resp, err
		}
		for _, r := range res.CheckRuns {
			runs = append(runs, CheckRun{
				Name: r.GetName(), Status: r.GetStatus(),
				Conclusion: r.GetConclusion(), DetailsURL: r.GetDetailsURL(),
			})
		}
		return resp, nil
	})
	if err != nil {
		return Checks{}, err
	}

	var statuses []Status
	err = paginate(func(page int) (*gogithub.Response, error) {
		st, resp, err := c.gh.Repositories.ListStatuses(ctx, c.Owner, c.Repo, sha,
			&gogithub.ListOptions{Page: page, PerPage: 100})
		c.note(resp)
		if err != nil {
			return resp, err
		}
		for _, s := range st {
			statuses = append(statuses, Status{
				Context: s.GetContext(), State: s.GetState(), Target: s.GetTargetURL(),
			})
		}
		return resp, nil
	})
	if err != nil {
		return Checks{}, err
	}

	return Checks{Ref: sha, Runs: runs, Statuses: statuses, HeadSHA: sha}, nil
}
