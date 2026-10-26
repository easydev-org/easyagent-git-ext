package ghclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	gogithub "github.com/google/go-github/v93/github"
)

// The step endpoints behind the GitHub web UI.
//
// WHY THESE ARE NOT IN api.github.com: the REST API returns a job's steps as {number, name,
// status, conclusion, started_at} — no UUID, and no per-step log address. The web UI uses two
// undocumented endpoints on github.com instead, and they answer ANONYMOUSLY for a public
// repository:
//
//	GET /{owner}/{repo}/actions/runs/{run}/jobs/{job}/steps?change_id=0
//	    every step, with its UUID (`id`), its live status, and — once it finishes — `log_url`
//
//	GET /{owner}/{repo}/actions/runs/{run}/jobs/{job}/steps/{uuid}/backscroll
//	    that step's log as JSON lines, ANSI colour intact
//
// THEY ARE UNDOCUMENTED, which is the honest risk: GitHub can change them without notice. That is
// why nothing else in this extension depends on them, and why every failure here is reported as
// "cannot read step logs" rather than being allowed to look like an empty log.
//
// WHAT THEY DO NOT DO: a step that is still RUNNING has no log_url and answers 404 on backscroll.
// Verified against a live job — GitHub itself reports log_url: null for in_progress steps. So
// these endpoints make finished logs nicer; they do not make live logs possible.

// webBase is where the web-UI endpoints live.
//
// Derived from the API base so GitHub Enterprise works too: an enterprise API root is
// https://host/api/v3, and the web UI is https://host.
func (c *Client) webBase() string {
	if c.apiBase == "" {
		return "https://github.com"
	}
	b := strings.TrimSuffix(c.apiBase, "/")
	b = strings.TrimSuffix(b, "/api/v3")
	return b
}

// StepDetail is one step as the web UI knows it.
type StepDetail struct {
	Number     int
	Name       string
	Status     string
	Conclusion string
	// UUID is the handle backscroll needs. The REST API never returns it.
	UUID string
	// StartedAt / CompletedAt are RFC3339, empty when not yet reached.
	StartedAt   string
	CompletedAt string
	// LogURL is nil until the step finishes: GitHub publishes a step's log only then, and says so
	// by leaving this null. An empty LogURL is therefore "not yet", not "no log".
	LogURL *string
}

// Finished reports whether this step has a log to read.
func (s StepDetail) Finished() bool { return s.LogURL != nil && *s.LogURL != "" }

// StepDetails lists a job's steps with their UUIDs and live status.
func (c *Client) StepDetails(ctx context.Context, runID, jobID int64) ([]StepDetail, error) {
	u := fmt.Sprintf("%s/%s/%s/actions/runs/%d/jobs/%d/steps?change_id=0",
		c.webBase(), c.Owner, c.Repo, runID, jobID)
	body, err := c.getWeb(ctx, u)
	if err != nil {
		return nil, err
	}
	var raw []struct {
		ID          string  `json:"id"`
		Name        string  `json:"name"`
		Status      string  `json:"status"`
		Conclusion  *string `json:"conclusion"`
		Number      int     `json:"number"`
		StartedAt   *string `json:"started_at"`
		CompletedAt *string `json:"completed_at"`
		LogURL      *string `json:"log_url"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("step list is not the expected JSON: %w", err)
	}
	out := make([]StepDetail, 0, len(raw))
	for _, r := range raw {
		d := StepDetail{
			Number: r.Number, Name: r.Name, Status: r.Status, UUID: r.ID,
			LogURL: r.LogURL,
		}
		if r.Conclusion != nil {
			d.Conclusion = *r.Conclusion
		}
		if r.StartedAt != nil {
			d.StartedAt = *r.StartedAt
		}
		if r.CompletedAt != nil {
			d.CompletedAt = *r.CompletedAt
		}
		out = append(out, d)
	}
	return out, nil
}

// StepLogLine is one line of a step's log.
type StepLogLine struct {
	ID   string
	Line string
}

// StepLog fetches one step's log as lines, ANSI colour preserved.
//
// Only a finished step has a log. A running one answers 404 — that is GitHub's answer, not a
// transient failure, so it is reported as such instead of being retried.
func (c *Client) StepLog(ctx context.Context, runID, jobID int64, uuid string, maxLines int) ([]StepLogLine, error) {
	if uuid == "" {
		return nil, fmt.Errorf("a step UUID is required; get one from StepDetails")
	}
	u := fmt.Sprintf("%s/%s/%s/actions/runs/%d/jobs/%d/steps/%s/backscroll",
		c.webBase(), c.Owner, c.Repo, runID, jobID, uuid)
	body, err := c.getWeb(ctx, u)
	if err != nil {
		if isNotFoundBody(err) {
			return nil, fmt.Errorf("no log for step %s yet: GitHub publishes a step's log only after "+
				"the step FINISHES, and has no endpoint for a running step's output", uuid)
		}
		return nil, err
	}
	var payload struct {
		Lines []struct {
			ID   string `json:"id"`
			Line string `json:"line"`
		} `json:"lines"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("step log is not the expected JSON: %w", err)
	}
	out := make([]StepLogLine, 0, len(payload.Lines))
	for _, l := range payload.Lines {
		out = append(out, StepLogLine{ID: l.ID, Line: l.Line})
	}
	// The tail, not the head: a failure is at the END, so trimming from the top would hide the
	// lines a caller needs.
	if maxLines > 0 && len(out) > maxLines {
		out = out[len(out)-maxLines:]
	}
	return out, nil
}

// JobInRun reports whether jobID belongs to runID.
//
// Checked before reading a step log because the web endpoints answer for job ids that the REST
// API no longer knows — an old attempt's job answers backscroll with STALE content and no
// indication that it is stale. Confirming membership is what keeps that from being reported as
// the current run's log.
func (c *Client) JobInRun(ctx context.Context, runID, jobID int64) (bool, error) {
	found := false
	err := paginate(func(page int) (*gogithub.Response, error) {
		jobs, resp, err := c.gh.Actions.ListWorkflowJobs(ctx, c.Owner, c.Repo, runID,
			&gogithub.ListWorkflowJobsOptions{ListOptions: gogithub.ListOptions{Page: page, PerPage: 100}})
		c.note(resp)
		if err != nil {
			return resp, err
		}
		for _, j := range jobs.Jobs {
			if j.GetID() == jobID {
				found = true
			}
		}
		return resp, nil
	})
	return found, err
}

// getWeb performs one anonymous GET against github.com.
//
// NO Authorization header: the endpoints answer anonymously for a public repository, and a token
// sent to the web host is ignored at best. Kept as its own method so the absence is deliberate
// and visible.
func (c *Client) getWeb(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "easyagent-github-ext")
	client := &http.Client{Timeout: 40 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &httpStatusError{Code: resp.StatusCode, URL: url, Body: string(body[:min(len(body), 200)])}
	}
	return body, nil
}

// httpStatusError carries the status so a caller can tell 404 from a failure.
type httpStatusError struct {
	Code int
	URL  string
	Body string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("GET %s: HTTP %d %s", e.URL, e.Code, e.Body)
}

func isNotFoundBody(err error) bool {
	var se *httpStatusError
	return errors.As(err, &se) && se.Code == http.StatusNotFound
}
