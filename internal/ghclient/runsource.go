package ghclient

import (
	"context"
	"strings"

	"github.com/easydev-org/easyagent-gitdev-ext/internal/uihandler"
)

// RunSource reads CI runs for the UI's run pages.
//
// SAME SHAPE AS PullSource AND IssueSource: a token resolver and a tenant lookup, a client per
// request so a configuration change applies to the next request rather than a restart.
type RunSource struct {
	// TokenFor returns the tenant's GitHub token, or "" when none is configured.
	TokenFor func(tenant string) string
	// TenantFrom resolves the caller for a request.
	TenantFrom func(ctx context.Context) (string, error)
}

func (s *RunSource) client(ctx context.Context, repo string) (*Client, error) {
	tenant := ""
	if s.TenantFrom != nil {
		t, err := s.TenantFrom(ctx)
		if err != nil {
			return nil, err
		}
		tenant = t
	}
	token := ""
	if s.TokenFor != nil {
		token = strings.TrimSpace(s.TokenFor(tenant))
	}
	return New(Options{Remote: "https://github.com/" + strings.Trim(repo, "/"), Token: token})
}

// toRunView maps one run, computing `Done` here rather than in three UI places.
func toRunView(r Run) uihandler.RunView {
	return uihandler.RunView{
		ID: r.ID, Name: r.Name, Status: r.Status, Conclusion: r.Conclusion,
		Branch: r.Branch, Event: r.Event, HeadSHA: r.HeadSHA, URL: r.URL,
		CreatedAt: r.CreatedAt, Attempt: r.Attempt,
		// `completed` is the only terminal status, and a completed run always has a conclusion —
		// but relying on that would make the UI crash on the day GitHub omits one.
		Done: r.Status == "completed",
	}
}

// Runs lists recent runs for a repository, newest first.
func (s *RunSource) Runs(ctx context.Context, repo string, limit int) ([]uihandler.RunView, error) {
	c, err := s.client(ctx, repo)
	if err != nil {
		return nil, err
	}
	list, err := c.ListRuns(ctx, ListRunsOptions{Limit: limit})
	if err != nil {
		return nil, err
	}
	out := make([]uihandler.RunView, 0, len(list))
	for _, r := range list {
		out = append(out, toRunView(r))
	}
	return out, nil
}

// Run returns one run's own facts.
func (s *RunSource) Run(ctx context.Context, repo string, id int64) (uihandler.RunView, error) {
	c, err := s.client(ctx, repo)
	if err != nil {
		return uihandler.RunView{}, err
	}
	r, err := c.Run(ctx, id)
	if err != nil {
		return uihandler.RunView{}, err
	}
	return toRunView(r), nil
}

// Jobs returns a run's jobs with their steps.
func (s *RunSource) Jobs(ctx context.Context, repo string, id int64) ([]uihandler.JobView, error) {
	c, err := s.client(ctx, repo)
	if err != nil {
		return nil, err
	}
	list, err := c.RunJobs(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]uihandler.JobView, 0, len(list))
	for _, j := range list {
		steps := make([]uihandler.StepView, 0, len(j.Steps))
		for i, st := range j.Steps {
			steps = append(steps, uihandler.StepView{
				Name: st.Name, Status: st.Status, Conclusion: st.Conclusion, Number: i + 1,
			})
		}
		out = append(out, uihandler.JobView{
			ID: j.ID, Name: j.Name, Status: j.Status, Conclusion: j.Conclusion,
			StartedAt: j.StartedAt, URL: j.URL, Steps: steps,
			// GitHub publishes a job's log only once the job has finished; a running job has none,
			// and offering a button that cannot work is worse than offering none.
			LogAvailable: j.Status == "completed",
		})
	}
	return out, nil
}

/**
 * JobLog returns one job's log.
 *
 * A LOG THAT IS NOT THERE YET is reported as ErrLogNotReady rather than as an error from GitHub: the
 * two are different facts — one means "wait", the other means "something broke" — and the page can
 * only say the right thing if it can tell them apart.
 */
func (s *RunSource) JobLog(ctx context.Context, repo string, jobID int64) (string, error) {
	c, err := s.client(ctx, repo)
	if err != nil {
		return "", err
	}
	// A TAIL, not the whole log. A long job produces megabytes, and sending all of it to a browser
	// that will show it in a scroll box is slow to transfer and slow to render — the end of a failed
	// job is also where the reason is. The reader can ask for more from the tool if they need it.
	text, _, err := c.JobLogText(ctx, jobID, JobLogOptions{Tail: 2000})
	if err != nil {
		// The client reports "no log" as a 404 from the logs endpoint; anything else is a real fault.
		if isNotFound(err) {
			return "", uihandler.ErrLogNotReady
		}
		return "", err
	}
	return text, nil
}

// isNotFound reports whether an error is GitHub saying the resource is not there.
func isNotFound(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "404") || strings.Contains(msg, "not found")
}
