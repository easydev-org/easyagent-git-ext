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
	if err := t.requireRepo(ctx, PermIssueCreate, repo); err != nil {
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

// issueList lists issues.
func (t *toolset) issueList(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	c, repo, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	items, lerr := c.ListIssues(ctx, ghclient.ListIssuesOptions{
		State: argStr(args, "state"), Label: argStr(args, "label"), Limit: argInt(args, "limit"),
	})
	if lerr != nil {
		return extension.ToolResultData{}, errFailed("%s: %v", repo, lerr)
	}
	if len(items) == 0 {
		return extension.ToolResultData{
			Content: "(no issues)",
			Data:    map[string]any{"issues": items, "count": 0},
		}, nil
	}
	var b strings.Builder
	for _, i := range items {
		mark := " "
		if i.State == "closed" {
			mark = "x"
		}
		fmt.Fprintf(&b, "%s #%-5d %-55s %s", mark, i.Number, truncate(i.Title, 55), i.Author)
		if i.Comments > 0 {
			fmt.Fprintf(&b, "  (%d comments)", i.Comments)
		}
		if len(i.Labels) > 0 {
			fmt.Fprintf(&b, "  [%s]", strings.Join(i.Labels, ","))
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "\n%d issue(s)", len(items))
	return extension.ToolResultData{
		Content: b.String(),
		Data:    map[string]any{"issues": items, "count": len(items)},
	}, nil
}

// issueShow shows one issue with its comments.
//
// The comments are included because an issue without its discussion is only the request, not the
// decision — a caller reading an issue to act on it needs what people said about it.
func (t *toolset) issueShow(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	c, repo, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	n := argInt(args, "number")
	if n <= 0 {
		return extension.ToolResultData{}, errArg(`"number" is required`)
	}
	issue, ierr := c.Issue(ctx, n)
	if ierr != nil {
		return extension.ToolResultData{}, errFailed("%s#%d: %v", repo, n, ierr)
	}
	comments, _ := c.IssueComments(ctx, n)

	var b strings.Builder
	fmt.Fprintf(&b, "#%d %s  [%s]\n", issue.Number, issue.Title, issue.State)
	fmt.Fprintf(&b, "by %s  %s\n", issue.Author, issue.CreatedAt)
	if len(issue.Labels) > 0 {
		fmt.Fprintf(&b, "labels: %s\n", strings.Join(issue.Labels, ", "))
	}
	fmt.Fprintf(&b, "%s\n", issue.HTMLURL)
	if strings.TrimSpace(issue.Body) != "" {
		fmt.Fprintf(&b, "\n%s\n", strings.TrimSpace(issue.Body))
	}
	if len(comments) > 0 {
		fmt.Fprintf(&b, "\n--- %d comment(s) ---\n", len(comments))
		for _, cm := range comments {
			fmt.Fprintf(&b, "\n[%s] %s\n%s\n", cm.Author, cm.When, strings.TrimSpace(cm.Body))
		}
	}
	return extension.ToolResultData{
		Content: b.String(),
		Data:    map[string]any{"issue": issue, "comments": comments},
	}, nil
}

// issueComment comments on an issue.
func (t *toolset) issueComment(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	repo, err := required(args, "repo")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	n := argInt(args, "number")
	if n <= 0 {
		return extension.ToolResultData{}, errArg(`"number" is required`)
	}
	body, err := required(args, "body")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	if err := t.requireRepo(ctx, PermIssueComment, repo); err != nil {
		return extension.ToolResultData{}, err
	}
	c, _, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	cm, cerr := c.CreateIssueComment(ctx, n, body)
	if cerr != nil {
		return extension.ToolResultData{}, errFailed("%s#%d: %v", repo, n, cerr)
	}
	return extension.ToolResultData{
		Content: fmt.Sprintf("commented on #%d\n%s", n, cm.URL),
		Data:    map[string]any{"issue": n, "comment": cm.ID, "url": cm.URL},
	}, nil
}

// issueClose closes an issue.
func (t *toolset) issueClose(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	c, repo, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	n := argInt(args, "number")
	if n <= 0 {
		return extension.ToolResultData{}, errArg(`"number" is required`)
	}
	if cerr := c.CloseIssue(ctx, n); cerr != nil {
		return extension.ToolResultData{}, errFailed("%s#%d: %v", repo, n, cerr)
	}
	return extension.ToolResultData{
		Content: fmt.Sprintf("closed issue #%d", n),
		Data:    map[string]any{"issue": n},
	}, nil
}

// commentPR comments on a pull request, optionally on a line.
func (t *toolset) commentPR(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	repo, err := required(args, "repo")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	n := argInt(args, "number")
	if n <= 0 {
		return extension.ToolResultData{}, errArg(`"number" is required`)
	}
	body, err := required(args, "body")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	if err := t.requireRepo(ctx, PermIssueComment, repo); err != nil {
		return extension.ToolResultData{}, err
	}
	c, _, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	cm, cerr := c.CreatePRComment(ctx, ghclient.CreatePRCommentOptions{
		Number: n, Body: body,
		Path: argStr(args, "path"), Line: argInt(args, "line"), Side: argStr(args, "side"),
	})
	if cerr != nil {
		return extension.ToolResultData{}, errFailed("%s#%d: %v", repo, n, cerr)
	}
	where := "the conversation"
	if cm.Path != "" {
		where = fmt.Sprintf("%s line %d", cm.Path, cm.Line)
	}
	return extension.ToolResultData{
		Content: fmt.Sprintf("commented on #%d at %s\n%s", n, where, cm.URL),
		Data:    map[string]any{"pr": n, "comment": cm.ID, "url": cm.URL, "path": cm.Path, "line": cm.Line},
	}, nil
}

// commentsPR shows what has been said on a pull request.
//
// BOTH kinds are returned and labelled: the conversation (where a reviewer writes a paragraph)
// and the line comments (where they point at code). Showing only one would let a caller reply
// into a thread it cannot see.
func (t *toolset) commentsPR(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	c, repo, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	n := argInt(args, "number")
	if n <= 0 {
		return extension.ToolResultData{}, errArg(`"number" is required`)
	}
	convo, cerr := c.IssueComments(ctx, n)
	if cerr != nil {
		return extension.ToolResultData{}, errFailed("%s#%d: %v", repo, n, cerr)
	}
	inline, _ := c.PRComments(ctx, n)

	if len(convo) == 0 && len(inline) == 0 {
		return extension.ToolResultData{
			Content: fmt.Sprintf("no comments on #%d", n),
			Data:    map[string]any{"conversation": convo, "inline": inline, "count": 0},
		}, nil
	}
	var b strings.Builder
	if len(convo) > 0 {
		fmt.Fprintf(&b, "=== conversation (%d) ===\n", len(convo))
		for _, cm := range convo {
			fmt.Fprintf(&b, "\n[%s] %s\n%s\n", cm.Author, cm.When, strings.TrimSpace(cm.Body))
		}
	}
	if len(inline) > 0 {
		fmt.Fprintf(&b, "\n=== line comments (%d) ===\n", len(inline))
		for _, cm := range inline {
			loc := cm.Path
			if cm.Line > 0 {
				loc = fmt.Sprintf("%s:%d", cm.Path, cm.Line)
			}
			fmt.Fprintf(&b, "\n[%s] %s  @ %s\n%s\n", cm.Author, cm.When, loc, strings.TrimSpace(cm.Body))
		}
	}
	return extension.ToolResultData{
		Content: b.String(),
		Data: map[string]any{
			"conversation": convo, "inline": inline,
			"count": len(convo) + len(inline),
		},
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
