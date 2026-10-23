package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/easydev-org/easyagent-gitea-ext/internal/config"
	"github.com/easydev-org/easyagent-gitea-ext/internal/stage"
	easybus "github.com/easydev-org/easybus-sdk"
	"github.com/easydev-org/easybus-sdk/extension"
	"github.com/easylab-platform/easyvcs/store"
)

// stageSubmit handles `git-stage-submit`: push the staged change to a branch and open a
// pull request, with NO clone and NO sandbox.
//
// This is the payoff of the staging area. The sandbox route is: start a container, clone
// the repository, run git three times, read the branch name back, call the forge API,
// stop the container. For a one-line change that is a minute of work and a container's
// worth of resources to produce a single commit.
func (t *toolset) stageSubmit(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	cfg := t.cfg(session, tenant)

	w, err := t.stageFor(ctx, session, tenant, t.stageRepoArg(args), argStr(args, "ref"))
	if err != nil {
		return extension.ToolResultData{}, stageErr(err)
	}
	files, err := w.Status()
	if err != nil {
		return extension.ToolResultData{}, stageErr(err)
	}
	if len(files) == 0 {
		// An empty submit would create an empty MR, and the forge would reject it with a
		// message about the branch rather than about the real problem.
		return extension.ToolResultData{}, &extension.TypedError{
			Code:    easybus.ToolResultErrorCodeInvalidArgument,
			Message: "nothing staged; use git-write / git-remove first",
		}
	}

	title := argStr(args, "title")
	if title == "" {
		return extension.ToolResultData{}, &extension.TypedError{
			Code: easybus.ToolResultErrorCodeInvalidArgument, Message: "title is required",
		}
	}
	body := argStr(args, "body")
	base := argStr(args, "base")
	if base == "" {
		base = cfg.DefaultBranch
	}
	if base == "" {
		base = "main"
	}
	branch := argStr(args, "branch")
	if branch == "" {
		// A fresh branch per submission: reusing a name would rewrite the head of an MR
		// a reviewer is already reading.
		branch = mrHeadBranch(session)
	}

	// One lookup serves both halves: the `servers` entry supplies the git credential for
	// the push and the API base for the pull request.
	remote := argStr(args, "remote")
	if remote == "" {
		return extension.ToolResultData{}, &extension.TypedError{
			Code:    easybus.ToolResultErrorCodeInvalidArgument,
			Message: "remote is required, e.g. https://git.example.com/owner/repo.git",
		}
	}
	cl, _, err := t.remoteClient(session, tenant, remote)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	srv, _ := cfg.ServerForHost(config.HostOf(remote))

	// Build and push a real git branch from database content (go-git, no git binary).
	res, err := w.Submit(ctx, stage.SubmitParams{
		Dest:        remote,
		Branch:      branch,
		Token:       srv.Token,
		Description: title,
		Author:      store.Author{Name: "agent", Email: "agent@easyagent.local"},
	})
	if err != nil {
		return extension.ToolResultData{}, stageErr(err)
	}

	pr, err := cl.OpenPullRequest(ctx, base, branch, title, prBody(body, files))
	if err != nil {
		return extension.ToolResultData{}, &extension.TypedError{
			// The push DID succeed, so say so: the caller must know a branch exists
			// rather than assume nothing happened.
			Code:    easybus.ToolResultErrorCodeInternal,
			Message: fmt.Sprintf("branch %s was pushed but the pull request failed: %v", branch, err),
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "pushed %s and opened pull request #%d\n%s\n\n", branch, pr.Index, pr.URL)
	b.WriteString(summarize(files))

	return extension.ToolResultData{
		Content: b.String(),
		Data: map[string]any{
			"branch": branch, "revision": res.Revision, "sha": res.SHA,
			"prIndex": pr.Index, "prUrl": pr.URL,
			"files": files, "base": base,
		},
	}, nil
}

// prBody adds a change summary so a reviewer sees the shape of the change without
// fetching the branch. It costs nothing: the change set is already in hand.
func prBody(body string, files []stage.FileStatus) string {
	if strings.TrimSpace(body) == "" {
		return summarize(files)
	}
	return body + "\n\n" + summarize(files)
}

// summarize renders a change set as a compact list.
func summarize(files []stage.FileStatus) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d file(s) changed:\n", len(files))
	for _, f := range files {
		mark := map[string]string{"added": "A", "modified": "M", "deleted": "D"}[f.Status]
		fmt.Fprintf(&b, "- %s %s", mark, f.Path)
		if f.Additions > 0 || f.Deletions > 0 {
			fmt.Fprintf(&b, " (+%d -%d)", f.Additions, f.Deletions)
		}
		b.WriteString("\n")
	}
	return b.String()
}
