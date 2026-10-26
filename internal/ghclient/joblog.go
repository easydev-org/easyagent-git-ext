package ghclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// A single job's log, fetched directly.
//
// WHY THIS EXISTS ALONGSIDE RunLogs: the run endpoint returns ONE zip containing every job, so
// reading it means unzipping to find the job you care about. The per-job endpoint returns that
// job's text directly — which is what a caller wants when a run reports "unit failed" and they
// need the output of THAT job.
//
// THE REDIRECT IS THE TRAP, and it cost real time to find: the endpoint answers 302 with a
// Location pointing at blob storage. A pre-signed URL REJECTS a request that carries an
// Authorization header, so a client that lets its GitHub client follow the redirect gets 401 and
// concludes "no permission" or "no logs". The Location must be fetched separately, with no auth
// header at all.
//
// LIVE LOGS DO NOT EXIST. Probed against a running workflow: the job endpoint answers 302 and the
// pre-signed URL answers 404 until the job completes, step-level logs answer 404 throughout, and
// the run zip is 404 as well. Only the job's step STATUS is live.

// JobLog returns one job's log text.
//
// maxLines caps the returned lines; 0 means no cap.
func (c *Client) JobLog(ctx context.Context, jobID int64, maxLines int) (string, error) {
	u, resp, err := c.gh.Actions.GetWorkflowJobLogs(ctx, c.Owner, c.Repo, jobID, 10)
	c.note(resp)
	if err != nil {
		if IsNotFound(err) {
			return "", fmt.Errorf("no log for job %d yet: GitHub publishes a job's log only after the job "+
				"FINISHES, and there is no API for a running job's output. Poll gh-run-jobs for the live "+
				"step status instead", jobID)
		}
		return "", wrap(err)
	}

	// Fetch the pre-signed URL with a plain client: no Authorization header. See the note above —
	// sending the token here is what turns a working download into a 401.
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return "", err
	}
	resp2, err := (&http.Client{Timeout: c.httpTimeout}).Do(req)
	if err != nil {
		return "", fmt.Errorf("download job log: %w", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download job log: HTTP %d", resp2.StatusCode)
	}
	phase(ctx, "reading the job log")
	body, err := io.ReadAll(io.LimitReader(resp2.Body, 32<<20))
	if err != nil {
		return "", fmt.Errorf("read job log: %w", err)
	}
	text := string(body)
	if maxLines > 0 {
		lines := strings.Split(text, "\n")
		if len(lines) > maxLines {
			// The tail, not the head: a failure is at the END of a log, so truncating from the
			// top would hide exactly the lines a caller needs.
			text = strings.Join(lines[len(lines)-maxLines:], "\n")
			text = fmt.Sprintf("... [showing the last %d of %d lines]\n", maxLines, len(lines)) + text
		}
	}
	return text, nil
}
