package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/easydev-org/easybus-sdk/extension"

	"github.com/easydev-org/easyagent-github-ext/internal/ghclient"
)

// CI: workflows, runs, jobs, logs.

// ghListRunsOptions maps tool arguments onto the client's filter.
func ghListRunsOptions(args map[string]any) ghclient.ListRunsOptions {
	return ghclient.ListRunsOptions{
		Branch: argStr(args, "branch"),
		Event:  argStr(args, "event"),
		Limit:  argInt(args, "limit"),
	}
}

func (t *toolset) workflowList(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	Progress(ctx).Phase("listing workflows")
	c, repo, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	wfs, err := c.ListWorkflows(ctx)
	if err != nil {
		return extension.ToolResultData{}, errFailed("%s: %v", repo, err)
	}
	var b strings.Builder
	for _, w := range wfs {
		fmt.Fprintf(&b, "%-28s %-34s %s\n", w.Name, w.Path, w.State)
	}
	if len(wfs) == 0 {
		b.WriteString("(no workflows)\n")
	}
	fmt.Fprintf(&b, "\n%d workflows", len(wfs))
	return extension.ToolResultData{
		Content: b.String(),
		Data:    map[string]any{"workflows": wfs, "count": len(wfs)},
	}, nil
}

func (t *toolset) runList(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	Progress(ctx).Phase("listing runs")
	c, repo, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	runs, err := c.ListRuns(ctx, ghListRunsOptions(args))
	if err != nil {
		return extension.ToolResultData{}, errFailed("%s: %v", repo, err)
	}
	var b strings.Builder
	for _, r := range runs {
		state := r.Status
		if r.Conclusion != "" {
			state = r.Conclusion
		}
		fmt.Fprintf(&b, "%-12d %-10s %-22s %-14s %s\n", r.ID, state, r.Name, r.Branch, shortSHA(r.HeadSHA))
	}
	if len(runs) == 0 {
		b.WriteString("(no runs)\n")
	}
	fmt.Fprintf(&b, "\n%d runs", len(runs))
	return extension.ToolResultData{
		Content: b.String(),
		Data:    map[string]any{"runs": runs, "count": len(runs)},
	}, nil
}

// runJobs shows where a run has got to — the LIVE view.
//
// The per-step status is available while the run is still going, which is the closest thing
// GitHub offers to watching CI: the LOG is only published once a job finishes.
func (t *toolset) runJobs(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	Progress(ctx).Phase("reading job status")
	c, repo, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	runID := int64(argInt(args, "run"))
	if runID <= 0 {
		return extension.ToolResultData{}, errArg(`"run" is required: the run id from gh-run-list`)
	}
	jobs, err := c.RunJobs(ctx, runID)
	if err != nil {
		return extension.ToolResultData{}, errFailed("%s run %d: %v", repo, runID, err)
	}
	var b strings.Builder
	for _, j := range jobs {
		state := j.Status
		if j.Conclusion != "" {
			state = j.Conclusion
		}
		fmt.Fprintf(&b, "%s  [%s]\n", j.Name, state)
		for _, s := range j.Steps {
			ss := s.Status
			if s.Conclusion != "" {
				ss = s.Conclusion
			}
			mark := " "
			switch ss {
			case "in_progress":
				mark = ">"
			case "failure":
				mark = "!"
			case "success":
				mark = "."
			}
			// The elapsed time is what turns "in_progress" into information: a step that has run
			// for nine seconds and one that has run for nine minutes are both "in_progress", and
			// only the second is worth worrying about.
			fmt.Fprintf(&b, "  %s %-40s %-12s%s\n", mark, s.Name, ss, stepElapsed(s))
		}
		b.WriteString("\n")
	}
	if len(jobs) == 0 {
		b.WriteString("(no jobs yet)\n")
	}
	return extension.ToolResultData{
		Content: b.String(),
		Data:    map[string]any{"jobs": jobs, "run": runID},
	}, nil
}

// runLog returns a run's logs, per job.
func (t *toolset) runLog(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	Progress(ctx).Phase("downloading the log archive")
	c, repo, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	runID := int64(argInt(args, "run"))
	if runID <= 0 {
		return extension.ToolResultData{}, errArg(`"run" is required: the run id from gh-run-list`)
	}
	// A default cap, because a full log of a matrix build can be tens of thousands of lines and
	// would drown the turn. The cap is reported when it bites.
	limit := argInt(args, "lines")
	if limit <= 0 {
		limit = 500
	}
	logs, err := c.RunLogs(progressCtx(ctx), runID, limit)
	if err != nil {
		return extension.ToolResultData{}, errFailed("%s run %d: %v", repo, runID, err)
	}
	var b strings.Builder
	for _, l := range logs {
		fmt.Fprintf(&b, "=== %s ===\n", l.Job)
		for _, ln := range l.Lines {
			b.WriteString(ln)
			b.WriteString("\n")
		}
		if l.Truncated {
			fmt.Fprintf(&b, "... [truncated at %d lines; raise `lines` for more]\n", limit)
		}
		b.WriteString("\n")
	}
	if len(logs) == 0 {
		b.WriteString("(no log content)\n")
	}
	return extension.ToolResultData{
		Content: b.String(),
		Data:    map[string]any{"run": runID, "jobs": len(logs)},
	}, nil
}

// jobLog returns ONE job's log.
//
// Separate from gh-run-log because the two answer different questions. A run's zip holds every
// job, so a caller reading it must unzip to find the one that failed; this fetches that job's
// text directly. Its tail is what matters — a failure is at the END of a log — so the line cap
// keeps the LAST lines rather than the first.
func (t *toolset) jobLog(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	Progress(ctx).Phase("downloading the job log")
	c, repo, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	jobID := int64(argInt(args, "job"))
	if jobID <= 0 {
		return extension.ToolResultData{}, errArg(`"job" is required: the job id from gh-run-jobs`)
	}
	limit := argInt(args, "lines")
	if limit <= 0 {
		limit = 500
	}
	text, lerr := c.JobLog(progressCtx(ctx), jobID, limit)
	if lerr != nil {
		return extension.ToolResultData{}, errFailed("%s job %d: %v", repo, jobID, lerr)
	}
	return extension.ToolResultData{
		Content: text,
		Data:    map[string]any{"job": jobID, "lines": limit},
	}, nil
}

// stepElapsed renders how long a step has taken, or how long it has been running.
//
// The format is short on purpose: it sits in a table beside the status, and its job is to make
// "in_progress" measurable — nine seconds and nine minutes are the same word otherwise.
func stepElapsed(s ghclient.Step) string {
	if s.StartedAt == "" {
		return ""
	}
	start, err := time.Parse("15:04:05", s.StartedAt)
	if err != nil {
		return ""
	}
	end := time.Now()
	if s.CompletedAt != "" {
		if e, eerr := time.Parse("15:04:05", s.CompletedAt); eerr == nil {
			end = e
		}
	}
	d := end.Sub(start)
	if d < 0 {
		// The step started before midnight and finished after it, and only the clock time was
		// kept. Reporting nothing is honest; reporting a negative duration is not.
		return ""
	}
	out := fmt.Sprintf("%ds", int(d.Seconds()))
	if d >= time.Minute {
		out = fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	if s.CompletedAt == "" {
		out += " +"
	}
	return out
}
