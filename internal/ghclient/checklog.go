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

// CheckRunLogsForRun lists the check runs attached to a workflow run's check suite.
//
// This is how the log-stream action's check run is found: it is not a job, so it does not appear
// in the run's job list, but it IS attached to the same suite.
func (c *Client) CheckRunLogsForRun(ctx context.Context, runID int64) ([]*CheckRunLog, error) {
	run, resp, err := c.gh.Actions.GetWorkflowRunByID(ctx, c.Owner, c.Repo, runID)
	c.note(resp)
	if err != nil {
		return nil, wrap(err)
	}
	suiteID := run.GetCheckSuiteID()
	if suiteID == 0 {
		return nil, fmt.Errorf("run %d has no check suite", runID)
	}
	out := []*CheckRunLog{}
	err = paginate(func(page int) (*gogithub.Response, error) {
		runs, resp2, err := c.gh.Checks.ListCheckRunsCheckSuite(ctx, c.Owner, c.Repo, suiteID,
			&gogithub.ListCheckRunsOptions{ListOptions: gogithub.ListOptions{Page: page, PerPage: 100}})
		c.note(resp2)
		if err != nil {
			return resp2, err
		}
		for _, cr := range runs.CheckRuns {
			out = append(out, toCheckRunLog(cr))
		}
		return resp2, nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func toCheckRunLog(cr *gogithub.CheckRun) *CheckRunLog {
	l := &CheckRunLog{
		Name:       cr.GetName(),
		Status:     cr.GetStatus(),
		Conclusion: cr.GetConclusion(),
		HTMLURL:    cr.GetHTMLURL(),
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
