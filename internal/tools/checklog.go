package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/easydev-org/easybus-sdk/extension"

	"github.com/easydev-org/easyagent-git-ext/internal/ghclient"
)

// gh-check-log: the LIVE view, and the three things a caller must know before trusting it.
//
// It is deliberately separate from gh-job-log, because they answer different questions and a single
// tool that answered both would have to be vague about which one it was answering:
//
//	gh-check-log  "what is the build doing right now"  — works while the job runs
//	gh-job-log    "what exactly happened"              — only after the job finishes
//
// The live one can only exist if the repository publishes it, so its description leads with that.
func (t *toolset) checkLog(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	c, repo, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}

	Progress(ctx).Phase("looking for a published stream")

	var logs []*ghclient.CheckRunLog
	jobID := int64(argInt(args, "job"))
	runID := int64(argInt(args, "run"))

	switch {
	case jobID > 0:
		one, jerr := c.CheckRunLogByJob(ctx, jobID)
		if jerr != nil {
			return extension.ToolResultData{}, errFailed("%s job %d: %v", repo, jobID, jerr)
		}
		logs = []*ghclient.CheckRunLog{one}
	case runID > 0:
		all, rerr := c.CheckRunLogsForRun(ctx, runID)
		if rerr != nil {
			return extension.ToolResultData{}, errFailed("%s run %d: %v", repo, runID, rerr)
		}
		logs = all
	default:
		return extension.ToolResultData{}, errArg(
			`give "run" (the run id from gh-run-list) or "job" (from gh-run-jobs)`)
	}

	var withText []*ghclient.CheckRunLog
	for _, l := range logs {
		if l.HasText() {
			withText = append(withText, l)
		}
	}

	if len(withText) == 0 {
		// Name the reason, because "empty" and "not published" look identical to a caller and only
		// one of them is actionable.
		var b strings.Builder
		fmt.Fprintf(&b, "nothing has been published for %s", repo)
		if runID > 0 {
			fmt.Fprintf(&b, " run %d", runID)
		} else {
			fmt.Fprintf(&b, " job %d", jobID)
		}
		fmt.Fprintf(&b, ".\n\n")
		fmt.Fprintf(&b, "A live log exists only if the workflow publishes one: this tool reads a "+
			"check run's output.text, which is empty unless the job uses the log-stream action.\n\n")
		fmt.Fprintf(&b, "  - uses: easydev-org/easyagent-git-ext/actions/log-stream@main\n")
		fmt.Fprintf(&b, "    with:\n")
		fmt.Fprintf(&b, "      token:  workflow-token-here\n")
		fmt.Fprintf(&b, "      command: make test\n\n")
		fmt.Fprintf(&b, "check runs seen (%d):\n", len(logs))
		for _, l := range logs {
			state := l.Status
			if l.Conclusion != "" {
				state = l.Conclusion
			}
			fmt.Fprintf(&b, "  %-28s %s\n", truncate(l.Name, 28), state)
		}
		fmt.Fprintf(&b, "\nfor the full log of a FINISHED job use gh-job-log.")
		return extension.ToolResultData{
			Content: b.String(),
			Data:    map[string]any{"published": false, "check_runs": len(logs)},
		}, nil
	}

	Progress(ctx).Phase("reading the published text")
	var b strings.Builder
	for i, l := range withText {
		if i > 0 {
			b.WriteString("\n")
		}
		state := l.Status
		if l.Conclusion != "" {
			state = l.Conclusion
		}
		fmt.Fprintf(&b, "=== %s  [%s] ===\n", l.Name, state)
		lines := strings.Split(l.Text, "\n")
		fmt.Fprintf(&b, "%d lines, tail only — the publisher keeps the newest and drops the rest\n\n", len(lines))
		b.WriteString(l.Text)
		if !strings.HasSuffix(l.Text, "\n") {
			b.WriteString("\n")
		}
	}
	b.WriteString("\nnote: this is the job's own command output as published by the workflow — " +
		"not the runner's full log. For the latter, gh-job-log once the job has finished.")

	return extension.ToolResultData{
		Content: b.String(),
		Data: map[string]any{
			"published": true, "check_runs": len(withText),
		},
	}, nil
}
