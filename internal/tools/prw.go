package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/easydev-org/easybus-sdk/extension"
)

// gh-pr-create: the ONLY way a change reaches GitHub.
//
// It PUBLISHES what is already committed: the workspace's current branch is pushed and turned
// into a pull request. Committing is `git-ws-commit`, kept separate on purpose — a session may
// commit several times before publishing, which is how work is actually done.
//
// So the division is: git-ws-commit (local, repeatable) → gh-pr-create (publish once).
func (t *toolset) prCreate(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	repo, err := required(args, "repo")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	title, err := required(args, "title")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	if err := t.requireRepo(ctx, PermPRCreate, repo); err != nil {
		return extension.ToolResultData{}, err
	}
	if t.token(tenant) == "" {
		return extension.ToolResultData{}, errArg("opening a pull request needs a GitHub token; set the `token` setting")
	}

	w, err := t.gws(args, session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	if w.Repo != repo {
		// Checked rather than assumed: a workspace of a DIFFERENT repository would push the
		// wrong content under this repository's name.
		return extension.ToolResultData{}, errArg(fmt.Sprintf(
			"workspace %s holds %s, not %s", w.ID, w.Repo, repo))
	}

	// Uncommitted changes are REFUSED rather than swept into the pull request: a caller that
	// edited and did not commit has not decided what the commit says, and publishing anyway
	// would make the commit message a lie.
	changed, serr := w.Status()
	if serr != nil {
		return extension.ToolResultData{}, errFailed("status: %v", serr)
	}
	if len(changed) > 0 {
		var b strings.Builder
		for _, f := range changed {
			fmt.Fprintf(&b, "  %-9s %s\n", f.Status, f.Path)
		}
		return extension.ToolResultData{}, errArg(
			"there are uncommitted changes:\n" + b.String() +
				"commit them with git-ws-commit first, so the pull request carries a message that describes it")
	}

	// The branch to publish. A workspace cloned at the default branch has main checked out, and
	// pushing THAT would be a direct push to main — so a fresh pr/* branch is created instead.
	branch := w.CurrentBranch()
	if branch == "" || branch == "main" || branch == "master" {
		fresh, berr := newBranchName()
		if berr != nil {
			return extension.ToolResultData{}, errFailed("branch name: %v", berr)
		}
		if cerr := w.CreateBranch(fresh); cerr != nil {
			return extension.ToolResultData{}, errFailed("create branch %s: %v", fresh, cerr)
		}
		branch = fresh
	}

	pushed, perr := w.Push(ctx, t.token(tenant))
	if perr != nil {
		return extension.ToolResultData{}, errFailed("%v", perr)
	}

	c, _, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	base := argStr(args, "base")
	if base == "" {
		info, ierr := c.Info(ctx)
		if ierr != nil {
			return extension.ToolResultData{}, errFailed("resolve default branch: %v", ierr)
		}
		base = info.DefaultBranch
	}
	pr, err := c.CreatePR(ctx, base, branch, title, argStr(args, "body"))
	if err != nil {
		// The branch exists; only the PR does not. Naming the branch lets the caller retry just
		// the pull request, and keeps an orphaned branch from going unnoticed.
		return extension.ToolResultData{}, errFailed(
			"pushed branch %s but could not open the pull request: %v (retry with base=%s)",
			branch, err, base)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "opened pull request #%d\n", pr.Number)
	fmt.Fprintf(&b, "%s\n", pr.HTMLURL)
	fmt.Fprintf(&b, "branch: %s -> %s\n", branch, base)
	fmt.Fprintf(&b, "commit: %s\n", shortSHA(pushed.SHA))
	b.WriteString("\nCI runs on the PR; check it with gh-pr-checks.")
	return extension.ToolResultData{
		Content: b.String(),
		Data: map[string]any{
			"pr": pr.Number, "url": pr.HTMLURL, "branch": branch, "base": base,
			"commit": pushed.SHA,
		},
	}, nil
}

// newBranchName makes a fresh branch name every time.
//
// Never reused: a second change that reused pr/<id> would rewrite the branch a reviewer is
// already reading, turning their review into a moving target.
func newBranchName() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "pr/" + hex.EncodeToString(b[:]), nil
}

// author is the commit identity used for agent-made commits.
func author(tenant string) struct{ Name, Email string } {
	return struct{ Name, Email string }{
		Name:  "easyagent",
		Email: "easyagent@" + tenant + ".local",
	}
}
