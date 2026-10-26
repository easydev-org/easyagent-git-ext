package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/easydev-org/easybus-sdk/extension"
)

// gh-step-log: one step's log, from the endpoint the web UI uses.
//
// WHY IT EXISTS ALONGSIDE gh-job-log: the REST API serves a job's log as a ZIP of plain text, so
// the caller gets neither colour nor line identity. This endpoint answers JSON, line by line, with
// ANSI escapes intact — the same bytes the GitHub log viewer renders.
//
// THE COST, stated plainly: it lives on github.com and is UNDOCUMENTED. It answers anonymously for
// a public repository, which is why it needs no token, but GitHub may change it without notice.
// When it breaks it says so; it does not fail quietly.
func (t *toolset) stepLog(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	c, repo, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	runID := int64(argInt(args, "run"))
	jobID := int64(argInt(args, "job"))
	if runID <= 0 || jobID <= 0 {
		return extension.ToolResultData{}, errArg(`"run" and "job" are required (from gh-run-jobs)`)
	}
	limit := argInt(args, "lines")
	if limit <= 0 {
		limit = 500
	}

	Progress(ctx).Phase("listing the steps")
	steps, serr := c.StepDetails(ctx, runID, jobID)
	if serr != nil {
		return extension.ToolResultData{}, errFailed("%s run %d job %d: %v", repo, runID, jobID, serr)
	}
	if len(steps) == 0 {
		return extension.ToolResultData{}, errFailed("%s run %d job %d: GitHub reported no steps",
			repo, runID, jobID)
	}

	// A step can be named, or numbered as GitHub numbers it.
	want := argInt(args, "step")
	name := argStr(args, "step_name")
	idx := -1
	for i := range steps {
		if want > 0 && steps[i].Number == want {
			idx = i
			break
		}
		if name != "" && strings.Contains(strings.ToLower(steps[i].Name), strings.ToLower(name)) {
			idx = i
			break
		}
	}
	if idx < 0 {
		// No selector: report what there is, so the caller can pick. Guessing a step would be
		// worse than a short list.
		var b strings.Builder
		fmt.Fprintf(&b, "which step? pass `step` (number) or `step_name`:\n\n")
		for _, s := range steps {
			state := s.Status
			if s.Conclusion != "" {
				state = s.Conclusion
			}
			mark := " "
			if s.Finished() {
				mark = "."
			}
			fmt.Fprintf(&b, "%s #%-3d %-46s %s\n", mark, s.Number, truncate(s.Name, 46), state)
		}
		b.WriteString("\na dot marks a step whose log is available.")
		return extension.ToolResultData{
			Content: b.String(),
			Data:    map[string]any{"steps": steps},
		}, nil
	}

	s := steps[idx]
	if !s.Finished() {
		// GitHub is explicit about this: an unfinished step has log_url null. Say so with the
		// step's own state, rather than returning an empty log a caller would read as "it printed
		// nothing".
		return extension.ToolResultData{}, errArg(fmt.Sprintf(
			"step #%d (%s) is %s: GitHub publishes a step's log only after it finishes, and has no "+
				"endpoint for a running step's output. Watch the step status with gh-run-jobs; "+
				"read the log once it completes",
			s.Number, s.Name, s.Status))
	}

	Progress(ctx).Phase("downloading the step log")
	lines, lerr := c.StepLog(ctx, runID, jobID, s.UUID, limit)
	if lerr != nil {
		return extension.ToolResultData{}, errFailed("%s step #%d: %v", repo, s.Number, lerr)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "step #%d %s  [%s]\n", s.Number, s.Name, s.Conclusion)
	if s.StartedAt != "" && s.CompletedAt != "" {
		if d := durationBetween(s.StartedAt, s.CompletedAt); d != "" {
			fmt.Fprintf(&b, "took %s\n", d)
		}
	}
	fmt.Fprintf(&b, "%d lines\n\n", len(lines))
	for _, l := range lines {
		b.WriteString(l.Line)
		b.WriteString("\n")
	}
	return extension.ToolResultData{
		Content: b.String(),
		Data: map[string]any{
			"run": runID, "job": jobID, "step": s.Number, "uuid": s.UUID,
			"lines": len(lines), "conclusion": s.Conclusion,
		},
	}, nil
}

// durationBetween renders an RFC3339 span as a human duration, or "" when unparseable.
func durationBetween(from, to string) string {
	a, err := time.Parse(time.RFC3339, from)
	if err != nil {
		return ""
	}
	b, err := time.Parse(time.RFC3339, to)
	if err != nil {
		return ""
	}
	d := b.Sub(a)
	if d < 0 {
		return ""
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}
