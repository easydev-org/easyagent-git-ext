package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/easydev-org/easybus-sdk/extension"

	"github.com/easydev-org/easyagent-github-ext/internal/ghclient"
)

// Issues and comments: the tools that let a caller SAY something rather than only look.

// issueCreate opens an issue.
func (t *toolset) issueCreate(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	repo, err := required(args, "repo")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	title, err := required(args, "title")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	c, _, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	issue, cerr := c.CreateIssue(ctx, ghclient.CreateIssueOptions{
		Title: title, Body: argStr(args, "body"), Labels: argSliceStr(args, "labels"),
	})
	if cerr != nil {
		return extension.ToolResultData{}, errFailed("%s: %v", repo, cerr)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "opened issue #%d\n%s\n", issue.Number, issue.HTMLURL)
	if len(issue.Labels) > 0 {
		fmt.Fprintf(&b, "labels: %s\n", strings.Join(issue.Labels, ", "))
	}
	return extension.ToolResultData{
		Content: b.String(),
		Data:    map[string]any{"issue": issue.Number, "url": issue.HTMLURL},
	}, nil
}

// truncate shortens a string for a one-line summary.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
