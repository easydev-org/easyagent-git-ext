package tools

import (
	"context"
	"fmt"
	"strings"

	easybus "github.com/easydev-org/easybus-sdk"
	"github.com/easydev-org/easybus-sdk/extension"

	"github.com/easydev-org/easyagent-gitea-ext/internal/forge"
)

// Merge-request actions: comment, show, close, reopen, merge.
//
// These are the other half of the review workflow: `git-submit-mr` opens a
// request, and these let the agent follow it up (answer a reviewer, read the
// thread, abandon a superseded submission, or land an approved one).

// mrClient resolves a forge client for an MR action, from `remote` when given or
// else from the repository at `path`.
//
// Every MR action accepts either form because the two halves of the workflow sit
// on different sides: git-submit-mr always has a sandbox repo, while "what did the
// reviewer say" should not require a sandbox at all.
func (t *toolset) mrClient(ctx context.Context, args map[string]any, session, tenant string) (*forge.Client, error) {
	remote := argStr(args, "remote")
	if remote == "" {
		r, path, err := t.repo(ctx, args, session, tenant)
		if err != nil {
			return nil, err
		}
		remote = r.RemoteURL(ctx, path)
	}
	cl, _, err := t.remoteClient(session, tenant, remote)
	return cl, err
}

// mrIndex reads the required `index` argument.
func mrIndex(args map[string]any) (int64, error) {
	idx := argInt(args, "index", 0)
	if idx <= 0 {
		return 0, &extension.TypedError{
			Code:    easybus.ToolResultErrorCodeInvalidArgument,
			Message: "index is required (the MR number, e.g. 7)",
		}
	}
	return int64(idx), nil
}

// mrShow reports one MR, with its comment thread.
func (t *toolset) mrShow(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	idx, err := mrIndex(args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	cl, err := t.mrClient(ctx, args, session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	pr, err := cl.GetPullRequest(ctx, idx)
	if err != nil {
		return extension.ToolResultData{}, &extension.TypedError{
			Code: easybus.ToolResultErrorCodeBusiness, Message: err.Error(),
		}
	}
	comments, err := cl.ListComments(ctx, idx)
	if err != nil {
		// The MR itself is the important part; a failing comment read must not
		// hide it.
		comments = nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "#%d %s\n  %s -> %s   state=%s\n  %s\n",
		pr.Index, pr.Title, pr.Head, pr.Base, pr.State, pr.URL)
	if len(comments) > 0 {
		fmt.Fprintf(&b, "\n  comments (%d):\n", len(comments))
		for _, c := range comments {
			body := strings.ReplaceAll(c.Body, "\n", "\n      ")
			fmt.Fprintf(&b, "    - %s: %s\n", c.Author, body)
		}
	}
	rows := make([]map[string]any, 0, len(comments))
	for _, c := range comments {
		rows = append(rows, map[string]any{"id": c.ID, "author": c.Author, "body": c.Body})
	}
	return extension.ToolResultData{
		Content: strings.TrimRight(b.String(), "\n"),
		Data: map[string]any{
			"index": pr.Index, "title": pr.Title, "state": pr.State,
			"head": pr.Head, "base": pr.Base, "url": pr.URL, "comments": rows,
		},
	}, nil
}

// mrComment posts a comment on an MR.
func (t *toolset) mrComment(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	idx, err := mrIndex(args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	body := argStr(args, "body")
	if strings.TrimSpace(body) == "" {
		return extension.ToolResultData{}, &extension.TypedError{
			Code: easybus.ToolResultErrorCodeInvalidArgument, Message: "body is required",
		}
	}
	cl, err := t.mrClient(ctx, args, session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	cm, err := cl.Comment(ctx, idx, body)
	if err != nil {
		return extension.ToolResultData{}, &extension.TypedError{
			Code: easybus.ToolResultErrorCodeBusiness, Message: err.Error(),
		}
	}
	return extension.ToolResultData{
		Content: fmt.Sprintf("commented on #%d (comment %d)", idx, cm.ID),
		Data:    map[string]any{"index": idx, "commentId": cm.ID},
	}, nil
}

// mrClose closes an MR without merging, or reopens it with `reopen: true`.
func (t *toolset) mrClose(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	idx, err := mrIndex(args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	// Closing is a write: it changes the review's state even though it does not merge.
	// Gated separately from mr.merge so a reviewer can be allowed to close a stale request
	// without being able to land one.
	if err := t.requireRepo(ctx, PermMRClose, args, session, tenant); err != nil {
		return extension.ToolResultData{}, err
	}
	cl, err := t.mrClient(ctx, args, session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	reopen := argBool(args, "reopen", false)
	var pr forge.PullRequest
	if reopen {
		pr, err = cl.ReopenPullRequest(ctx, idx)
	} else {
		pr, err = cl.ClosePullRequest(ctx, idx)
	}
	if err != nil {
		return extension.ToolResultData{}, &extension.TypedError{
			Code: easybus.ToolResultErrorCodeBusiness, Message: err.Error(),
		}
	}
	verb := "closed"
	if reopen {
		verb = "reopened"
	}
	return extension.ToolResultData{
		Content: fmt.Sprintf("%s #%d (state=%s)", verb, pr.Index, pr.State),
		Data:    map[string]any{"index": pr.Index, "state": pr.State},
	}, nil
}

// mrMerge merges an MR.
//
// This is the one MR action that changes the base branch, so it refuses to run
// unless the caller names the MR explicitly (index is required) and otherwise
// leaves style/branch-deletion to the forge's defaults.
func (t *toolset) mrMerge(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	idx, err := mrIndex(args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	// Permission gate, BEFORE any forge call. Checked here rather than trusted from the
	// agent because the token lives in this process: this is the only place that can stop
	// the merge from happening, and the target (which repository) is only known here.
	if err := t.requireRepo(ctx, PermMRMerge, args, session, tenant); err != nil {
		return extension.ToolResultData{}, err
	}
	cl, err := t.mrClient(ctx, args, session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	style := argStr(args, "style")
	switch style {
	case "", "merge", "rebase", "rebase-merge", "squash", "fast-forward-only":
	default:
		return extension.ToolResultData{}, &extension.TypedError{
			Code: easybus.ToolResultErrorCodeInvalidArgument,
			Message: fmt.Sprintf("style %q is not a Gitea merge style "+
				"(merge | rebase | rebase-merge | squash | fast-forward-only)", style),
		}
	}
	// Default to deleting the agent's head branch: without it every submission
	// leaves an agent/* branch behind forever.
	deleteBranch := argBool(args, "delete-branch", true)
	pr, err := cl.MergePullRequest(ctx, idx, style, argStr(args, "title"), argStr(args, "message"), deleteBranch)
	if err != nil {
		return extension.ToolResultData{}, &extension.TypedError{
			Code: easybus.ToolResultErrorCodeBusiness, Message: err.Error(),
		}
	}
	return extension.ToolResultData{
		Content: fmt.Sprintf("merged #%d (state=%s, head branch deleted=%v)", pr.Index, pr.State, deleteBranch),
		Data: map[string]any{
			"index": pr.Index, "state": pr.State,
			"style": style, "deleteBranch": deleteBranch,
		},
	}, nil
}
