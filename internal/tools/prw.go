package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/easydev-org/easybus-sdk/extension"
)

/**
 * gh-pr-create: open a pull request for a branch that is ALREADY on GitHub.
 *
 * WHY IT NO LONGER PUSHES. It used to take a workspace, commit it, push a fresh branch and then open
 * the pull request — three steps behind one name. That worked only while this extension owned a git
 * checkout. It no longer does: the mirror is changed through the forge's API and pushed with
 * `git-push-branch`, which is the one thing that knows which branch to send where.
 *
 * So the split is now honest, and each tool does one thing:
 *
 *	git-branch-create + git-file-patch   change the mirror
 *	git-push-branch                      send that branch to GitHub
 *	gh-pr-create                         open the request
 *
 * `head` is REQUIRED and is not guessed. A caller that just pushed knows the name; one that did not
 * would be opening a request for a branch that may not exist, and the error for that is far less
 * clear than a missing argument.
 */
func (t *toolset) prCreate(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	repo, err := required(args, "repo")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	title, err := required(args, "title")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	head := strings.TrimSpace(argStr(args, "head"))
	if head == "" {
		return extension.ToolResultData{}, errArg(
			"head is required: the branch that already exists on GitHub " +
				"(push it first with git-push-branch)")
	}
	if err := t.requireRepo(ctx, PermMaintainer, repo); err != nil {
		return extension.ToolResultData{}, err
	}
	if t.token(tenant) == "" {
		return extension.ToolResultData{}, errArg("opening a pull request needs a GitHub token; set the `token` setting")
	}

	c, _, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	base := strings.TrimSpace(argStr(args, "base"))
	if base == "" {
		// Resolved rather than assumed: the default branch differs between repositories, and
		// opening a request into a guessed branch fails with a message about a missing branch.
		info, ierr := c.Info(ctx)
		if ierr != nil {
			return extension.ToolResultData{}, errFailed("resolve default branch: %v", ierr)
		}
		base = info.DefaultBranch
	}
	if head == base {
		return extension.ToolResultData{}, errArg(fmt.Sprintf(
			"head and base are both %q: a pull request needs two different branches", base))
	}

	Progress(ctx).Phase("opening the pull request")
	pr, err := c.CreatePR(ctx, base, head, title, argStr(args, "body"))
	if err != nil {
		// The branch state is the common cause and is worth naming: the caller can then check
		// whether the push happened, rather than re-reading a generic API error.
		return extension.ToolResultData{}, errFailed(
			"could not open a pull request for %s -> %s: %v (was %s pushed?)",
			head, base, err, head)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "opened pull request #%d\n", pr.Number)
	fmt.Fprintf(&b, "%s\n", pr.HTMLURL)
	fmt.Fprintf(&b, "branch: %s -> %s\n", head, base)
	b.WriteString("\nCI runs on the PR; check it with gh-pr-checks.")
	return extension.ToolResultData{
		Content: b.String(),
		Data: map[string]any{
			"pr": pr.Number, "url": pr.HTMLURL, "branch": head, "base": base,
			"view": pullView(repo, pr.Number, fmt.Sprintf("%s#%d", repo, pr.Number)),
		},
	}, nil
}
