package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	easybus "github.com/easydev-org/easybus-sdk"
	"github.com/easydev-org/easybus-sdk/extension"
)

// Review-request workflow: `git-push` is refused, and the ONLY way to get changes
// onto the remote is `git-submit-mr`.
//
// WHY: an agent pushing straight to a shared branch makes unreviewable history.
// A pull/merge request costs one extra branch and gives a human a diff, a
// comment thread and an approve/merge step.
//
// HOW (entirely inside the sandbox, no gateway):
//  1. `git push` the current branch to a NEW immutable head branch
//     `agent/<session-scoped-slug>` — never to the base branch.
//  2. open the MR through the forge API (head -> base).
//
// The head name is derived from the session so two agents working concurrently
// cannot collide, and carries a unique suffix so a resubmit of the same session
// makes a new head instead of force-updating one an MR already points at.

// mrBranchPrefix marks head branches this extension creates.
const mrBranchPrefix = "agent/"

// submitMR handles `git-submit-mr`.
func (t *toolset) submitMR(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	r, path, err := t.repo(ctx, args, session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	base := argStr(args, "base")
	if base == "" {
		base = t.cfg(session, tenant).DefaultBranch
	}
	title := argStr(args, "title")
	if title == "" {
		return extension.ToolResultData{}, &extension.TypedError{
			Code: easybus.ToolResultErrorCodeInvalidArgument, Message: "title is required",
		}
	}
	body := argStr(args, "body")

	// The local branch must have something the base does not, or the MR would be
	// empty and the forge would reject it with a confusing message.
	head, err := r.Git(ctx, path, 30*time.Second, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	local := strings.TrimSpace(head)
	if local == "" || local == "HEAD" {
		return extension.ToolResultData{}, &extension.TypedError{
			Code:    easybus.ToolResultErrorCodeInvalidArgument,
			Message: "the repository is on a detached HEAD; check out a branch before submitting",
		}
	}
	// Commits present locally but not on the base branch.
	countOut, err := r.Git(ctx, path, 30*time.Second, "rev-list", "--count", "origin/"+base+"..HEAD")
	if err != nil {
		// origin/<base> may not exist yet (a fresh clone of an empty repo, or a
		// base that was never fetched). Fall back to comparing against the local
		// base ref so the check does not block a legitimate submission.
		countOut, err = r.Git(ctx, path, 30*time.Second, "rev-list", "--count", base+"..HEAD")
		if err != nil {
			return extension.ToolResultData{}, err
		}
	}
	count := strings.TrimSpace(countOut)
	if count == "0" {
		return extension.ToolResultData{}, &extension.TypedError{
			Code:    easybus.ToolResultErrorCodeInvalidArgument,
			Message: fmt.Sprintf("the current branch has no commits that %q does not already have; commit your changes first", base),
		}
	}

	// Build the immutable head branch name from the session, so concurrent
	// sessions cannot collide and a resubmit creates a new head rather than
	// rewriting one an open MR already points at.
	headBranch := mrHeadBranch(session)

	remote := r.RemoteURL(ctx, path)
	auth := t.authFor(session, tenant, remote)
	push := "git -C " + shellQuote(path) + " push " + shellQuote(remote) + " " +
		shellQuote(local) + ":" + shellQuote("refs/heads/"+headBranch)
	if auth.token != "" {
		push = gitWithToken(push, auth.user)
		if _, err := r.RunSecret(ctx, "", push, auth.token, 300*time.Second); err != nil {
			return extension.ToolResultData{}, err
		}
	} else {
		if _, err := r.Run(ctx, "", push, 300*time.Second); err != nil {
			return extension.ToolResultData{}, err
		}
	}

	// Open the MR over the API. This is the same forge client the remote-read
	// tools use, so there is no gateway in the path.
	cl, _, err := t.remoteClient(session, tenant, remote)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	pr, err := cl.OpenPullRequest(ctx, base, headBranch, title, body)
	if err != nil {
		return extension.ToolResultData{}, &extension.TypedError{
			Code:    easybus.ToolResultErrorCodeBusiness,
			Message: fmt.Sprintf("pushed %s but could not open the MR: %v", headBranch, err),
		}
	}

	return extension.ToolResultData{
		Content: fmt.Sprintf("MR #%d opened: %s -> %s (%s commit(s) on %s)\n%s",
			pr.Index, headBranch, base, count, local, pr.URL),
		Data: map[string]any{
			"index": pr.Index, "url": pr.URL,
			"head": headBranch, "base": base, "commits": count,
		},
	}, nil
}

// mrHeadBranch derives the immutable head branch for a submission.
//
// Shape: agent/<session>-<unixnano>. The session keeps concurrent agents apart;
// the timestamp makes each submission a NEW branch, so an MR never has its head
// rewritten underneath a reviewer.
func mrHeadBranch(session string) string {
	slug := sanitizeBranchPart(session)
	if slug == "" {
		slug = "session"
	}
	return fmt.Sprintf("%s%s-%d", mrBranchPrefix, slug, time.Now().UnixNano())
}

// sanitizeBranchPart keeps only characters that are safe in a git ref.
func sanitizeBranchPart(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == '/' || r == '.' || r == ' ':
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 40 {
		out = out[:40]
	}
	return out
}

// refusePush is what `git-push` now does: it explains the workflow instead of
// silently disappearing from the tool list, so a caller that remembers the old
// name is told what to do rather than left guessing.
func (t *toolset) refusePush(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	_ = ctx
	_ = args
	_ = session
	_ = tenant
	return extension.ToolResultData{}, &extension.TypedError{
		Code: easybus.ToolResultErrorCodePermissionDenied,
		Message: "direct push is disabled: changes reach the remote only through review. " +
			"Use git-submit-mr (it pushes to an agent/* head branch and opens a merge request).",
	}
}

// listMRs handles `git-mr-list`.
func (t *toolset) listMRs(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	remote := argStr(args, "remote")
	if remote == "" {
		// Default to the repository this session already has checked out.
		r, path, err := t.repo(ctx, args, session, tenant)
		if err != nil {
			return extension.ToolResultData{}, err
		}
		remote = r.RemoteURL(ctx, path)
	}
	cl, _, err := t.remoteClient(session, tenant, remote)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	state := argStr(args, "state")
	if state == "" {
		state = "open"
	}
	prs, err := cl.ListPullRequests(ctx, state)
	if err != nil {
		return extension.ToolResultData{}, &extension.TypedError{
			Code: easybus.ToolResultErrorCodeBusiness, Message: err.Error(),
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s/%s (%s)\n", cl.Owner, cl.Repo, state)
	if len(prs) == 0 {
		b.WriteString("  (none)")
	}
	rows := make([]map[string]any, 0, len(prs))
	for _, p := range prs {
		fmt.Fprintf(&b, "  #%-4d %-9s %s -> %s  %s\n", p.Index, p.State, p.Head, p.Base, p.Title)
		rows = append(rows, map[string]any{
			"index": p.Index, "title": p.Title, "state": p.State,
			"head": p.Head, "base": p.Base, "url": p.URL,
		})
	}
	return extension.ToolResultData{
		Content: strings.TrimRight(b.String(), "\n"),
		Data:    map[string]any{"owner": cl.Owner, "repo": cl.Repo, "pullRequests": rows},
	}, nil
}
