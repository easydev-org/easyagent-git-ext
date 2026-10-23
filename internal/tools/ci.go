package tools

import (
	"context"
	"fmt"
	"strings"

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
			fmt.Fprintf(&b, "  %s %-40s %s\n", mark, s.Name, ss)
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
	logs, err := c.RunLogs(ctx, runID, limit)
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
