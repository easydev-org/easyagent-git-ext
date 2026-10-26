package ghclient

import (
	"context"
	"fmt"
	"strings"

	gogithub "github.com/google/go-github/v93/github"
)

// CheckRunLog reads the text a check run carries, which is the only live log a token can see.
//
// WHY THIS EXISTS: a running job's Actions log is not readable through any public API. GitHub keeps
// it in an internal store and serves it only once the job finishes; the web endpoints that the
// viewer uses want a browser session cookie. A check run's output.text, by contrast, answers a
// plain GET while the job runs — but only because SOMEBODY PUT IT THERE. It holds whatever the
// workflow chose to publish, so it is empty unless that workflow uses the log-stream action.
type CheckRunLog struct {
	// Name, Status and Conclusion describe the check run itself.
	Name       string
	Status     string
	Conclusion string
	// Title, Summary and Text are the output object; Text is the streamed log.
	Title   string
	Summary string
	Text    string
	// HTMLURL points at the check run on github.com.
	HTMLURL string
	// ExternalID is what the publisher set it to; the log-stream action puts the workflow RUN ID
	// here, because nothing else in a check run says which run created it.
	ExternalID string
	// fromThisRun marks a check run whose external_id names the run that was asked about.
	fromThisRun bool
}

// CheckRunLogByJob reads the check run whose id equals the job id.
//
// A job and its own check run share an id, which is what makes this reachable from a tool that
// already knows the job. The check run the log-stream action creates has a DIFFERENT id — it is a
// separate check run — so callers that know only the run id should use CheckRunLogsForRun.
func (c *Client) CheckRunLogByJob(ctx context.Context, jobID int64) (*CheckRunLog, error) {
	cr, resp, err := c.gh.Checks.GetCheckRun(ctx, c.Owner, c.Repo, jobID)
	c.note(resp)
	if err != nil {
		if IsNotFound(err) {
			return nil, fmt.Errorf("no check run %d", jobID)
		}
		return nil, wrap(err)
	}
	return toCheckRunLog(cr), nil
}

// CheckRunLogsForRun lists the check runs published against a workflow run's commit.
//
// WHY NOT THE CHECK SUITE: the obvious route — run → check_suite_id → its check runs — returns only
// the check run GitHub created for the job itself, which is empty. A check run that a step POSTs is
// attached to the same COMMIT but not to that suite. Measured on one run:
//
//	check suite of the run   → 121637275523  text_len=0
//	check runs on the commit → 121637337009  text_len=844   <- the published one
//
// So the search goes by commit. That also means callers see every check run on that commit,
// including ones from earlier runs of the same workflow; the caller filters by HasText.
func (c *Client) CheckRunLogsForRun(ctx context.Context, runID int64) ([]*CheckRunLog, error) {
	run, resp, err := c.gh.Actions.GetWorkflowRunByID(ctx, c.Owner, c.Repo, runID)
	c.note(resp)
	if err != nil {
		return nil, wrap(err)
	}
	sha := run.GetHeadSHA()
	if sha == "" {
		return nil, fmt.Errorf("run %d has no head sha", runID)
	}
	out := []*CheckRunLog{}
	wantRun := fmt.Sprintf("%d", runID)
	err = paginate(func(page int) (*gogithub.Response, error) {
		runs, resp2, err := c.gh.Checks.ListCheckRunsForRef(ctx, c.Owner, c.Repo, sha,
			&gogithub.ListCheckRunsOptions{
				Filter:      gogithub.String("all"),
				ListOptions: gogithub.ListOptions{Page: page, PerPage: 100},
			})
		c.note(resp2)
		if err != nil {
			return resp2, err
		}
		for _, cr := range runs.CheckRuns {
			l := toCheckRunLog(cr)
			// Prefer the ones this run published. A commit can carry check runs from earlier runs
			// of the same workflow, and showing those would report a different build's output as
			// if it were this one — the worst kind of wrong answer here, because it looks valid.
			if l.ExternalID == wantRun {
				l.fromThisRun = true
			}
			out = append(out, l)
		}
		return resp2, nil
	})
	if err != nil {
		return nil, err
	}
	// If nothing declares this run, fall back to everything on the commit: an older action version
	// did not set external_id, and refusing to show anything would punish the caller for that.
	anyFromRun := false
	for _, l := range out {
		if l.fromThisRun {
			anyFromRun = true
			break
		}
	}
	if anyFromRun {
		kept := out[:0]
		for _, l := range out {
			if l.fromThisRun {
				kept = append(kept, l)
			}
		}
		out = kept
	}
	return out, nil
}

func toCheckRunLog(cr *gogithub.CheckRun) *CheckRunLog {
	l := &CheckRunLog{
		Name:       cr.GetName(),
		Status:     cr.GetStatus(),
		Conclusion: cr.GetConclusion(),
		HTMLURL:    cr.GetHTMLURL(),
		ExternalID: cr.GetExternalID(),
	}
	// Output is a pointer struct; text is only present when somebody wrote it.
	if o := cr.GetOutput(); o != nil {
		l.Title = o.GetTitle()
		l.Summary = o.GetSummary()
		l.Text = o.GetText()
	}
	return l
}

// HasText reports whether this check run carries anything to show.
func (l *CheckRunLog) HasText() bool { return strings.TrimSpace(l.Text) != "" }
