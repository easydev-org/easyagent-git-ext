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

// JobLogOptions selects a window of a finished job's log.
//
// The whole log is fetched either way — GitHub serves it as one archive, there is no ranged read —
// but what a caller sees is a slice, because a build log is thousands of lines and the useful part
// is almost never the beginning.
type JobLogOptions struct {
	// Tail shows only the last N lines. Ignored when Offset is set.
	Tail int
	// Offset, when > 0, shows from that line (1-based) instead of the tail.
	Offset int
	// Limit caps the window; 0 means "the rest".
	Limit int
	// Grep keeps only lines containing this substring, applied before the window.
	Grep string
	// IgnoreCase folds the case of Grep.
	IgnoreCase bool
}

// JobLogText returns the selected window plus a note saying what is not being shown.
func (c *Client) JobLogText(ctx context.Context, jobID int64, o JobLogOptions) (string, int, error) {
	raw, err := c.jobLogRaw(ctx, jobID)
	if err != nil {
		return "", 0, err
	}

	lines := strings.Split(strings.TrimSuffix(raw, "\n"), "\n")
	total := len(lines)

	if o.Grep != "" {
		needle := o.Grep
		if o.IgnoreCase {
			needle = strings.ToLower(needle)
		}
		var kept []string
		for _, l := range lines {
			hay := l
			if o.IgnoreCase {
				hay = strings.ToLower(hay)
			}
			if strings.Contains(hay, needle) {
				kept = append(kept, l)
			}
		}
		if len(kept) == 0 {
			return fmt.Sprintf("no line of job %d contains %q (%d lines searched)", jobID, o.Grep, total), total, nil
		}
		lines = kept
	}

	matched := len(lines)
	start := 0
	switch {
	case o.Offset > 0:
		start = o.Offset - 1
	case o.Tail > 0 && matched > o.Tail:
		start = matched - o.Tail
	}
	if start >= matched {
		return fmt.Sprintf("job %d has %d lines; offset %d is past the end", jobID, matched, o.Offset), total, nil
	}
	end := matched
	if o.Limit > 0 && start+o.Limit < end {
		end = start + o.Limit
	}

	shown := lines[start:end]
	var b strings.Builder
	// Say what is missing BEFORE the text, with counts: "some lines omitted" tells a reader nothing
	// about whether they are seeing a rounding error or the middle of a stack trace.
	if o.Grep != "" {
		fmt.Fprintf(&b, "[%d of %d lines match %q]\n", matched, total, o.Grep)
	}
	if start > 0 {
		fmt.Fprintf(&b, "[%d lines omitted before this window]\n", start)
	}
	if end < matched {
		fmt.Fprintf(&b, "[%d lines omitted after this window — pass offset=%d to continue]\n", matched-end, end+1)
	}
	b.WriteString(strings.Join(shown, "\n"))
	b.WriteString("\n")
	return b.String(), total, nil
}

// jobLogRaw downloads and unpacks a finished job's log.
func (c *Client) jobLogRaw(ctx context.Context, jobID int64) (string, error) {
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
	return string(body), nil
}
