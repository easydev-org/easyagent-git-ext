package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/easydev-org/easybus-sdk/extension"

	"github.com/easydev-org/easyagent-github-ext/internal/ghclient"
)

// Reviewing pull requests. These are reads except merge and close, which are gated.

func (t *toolset) prList(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	c, repo, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	list, err := c.ListPRs(ctx, argStr(args, "state"))
	if err != nil {
		return extension.ToolResultData{}, errFailed("%s: %v", repo, err)
	}
	var b strings.Builder
	for _, pr := range list {
		fmt.Fprintf(&b, "#%d %s [%s] %s -> %s\n", pr.Number, pr.Title, pr.State, pr.Head, pr.Base)
	}
	if len(list) == 0 {
		b.WriteString("(no pull requests)\n")
	}
	fmt.Fprintf(&b, "\n%d pull requests", len(list))
	return extension.ToolResultData{
		Content: b.String(),
		Data:    map[string]any{"prs": list, "count": len(list)},
	}, nil
}

func (t *toolset) prShow(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	c, repo, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	n := argInt(args, "number")
	if n <= 0 {
		return extension.ToolResultData{}, errArg(`"number" is required`)
	}
	pr, err := c.PR(ctx, n)
	if err != nil {
		return extension.ToolResultData{}, errFailed("%s#%d: %v", repo, n, err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "#%d %s\n", pr.Number, pr.Title)
	fmt.Fprintf(&b, "%s\n", pr.HTMLURL)
	fmt.Fprintf(&b, "state: %s   %s -> %s\n", pr.State, pr.Head, pr.Base)
	if pr.Merged {
		b.WriteString("merged\n")
	}
	// Mergeable is tri-state: nil means GitHub is still working it out, and reporting that as
	// "no" would be wrong.
	if pr.Mergeable != nil {
		fmt.Fprintf(&b, "mergeable: %v\n", *pr.Mergeable)
	} else {
		b.WriteString("mergeable: (computing)\n")
	}
	return extension.ToolResultData{
		Content: b.String(),
		Data:    map[string]any{"pr": pr},
	}, nil
}

func (t *toolset) prChecks(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	c, repo, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	n := argInt(args, "number")
	if n <= 0 {
		return extension.ToolResultData{}, errArg(`"number" is required`)
	}
	ch, err := c.PRChecks(ctx, n)
	if err != nil {
		return extension.ToolResultData{}, errFailed("%s#%d: %v", repo, n, err)
	}

	state := ch.State()
	var b strings.Builder
	fmt.Fprintf(&b, "CI: %s\n", state)
	if state == "none" {
		b.WriteString("(no checks and no status contexts have reported for this commit)\n")
	}
	for _, r := range ch.Runs {
		fmt.Fprintf(&b, "  %-30s %s/%s\n", r.Name, r.Status, r.Conclusion)
	}
	for _, s := range ch.Statuses {
		fmt.Fprintf(&b, "  %-30s status: %s\n", s.Context, s.State)
	}
	return extension.ToolResultData{
		Content: b.String(),
		Data:    map[string]any{"state": state, "checks": ch, "sha": ch.HeadSHA},
	}, nil
}

func (t *toolset) prMerge(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	repo, err := required(args, "repo")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	if err := t.requireRepo(ctx, PermPRMerge, repo); err != nil {
		return extension.ToolResultData{}, err
	}
	n := argInt(args, "number")
	if n <= 0 {
		return extension.ToolResultData{}, errArg(`"number" is required`)
	}
	c, _, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	pr, err := c.MergePR(ctx, n, argStr(args, "method"), argStr(args, "title"), argStr(args, "message"))
	if err != nil {
		return extension.ToolResultData{}, errFailed("%s#%d: %v", repo, n, err)
	}
	return extension.ToolResultData{
		Content: fmt.Sprintf("merged #%d\n%s", pr.Number, pr.HTMLURL),
		Data:    map[string]any{"pr": pr},
	}, nil
}

func (t *toolset) prClose(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	repo, err := required(args, "repo")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	if err := t.requireRepo(ctx, PermPRClose, repo); err != nil {
		return extension.ToolResultData{}, err
	}
	n := argInt(args, "number")
	if n <= 0 {
		return extension.ToolResultData{}, errArg(`"number" is required`)
	}
	c, _, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	pr, err := c.ClosePR(ctx, n)
	if err != nil {
		return extension.ToolResultData{}, errFailed("%s#%d: %v", repo, n, err)
	}
	return extension.ToolResultData{
		Content: fmt.Sprintf("closed #%d\n%s", pr.Number, pr.HTMLURL),
		Data:    map[string]any{"pr": pr},
	}, nil
}

// Repository administration.

func (t *toolset) repoCreate(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	name, err := required(args, "name")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	org := argStr(args, "org")
	slug := name
	if org != "" {
		slug = org + "/" + name
	}
	if err := t.requireRepo(ctx, PermRepoCreate, slug); err != nil {
		return extension.ToolResultData{}, err
	}
	c, err := t.accountClient(tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	r, err := c.CreateRepo(ctx, ghclient.CreateRepoOptions{
		Org: org, Name: name,
		Description: argStr(args, "description"),
		Private:     argBool(args, "private"),
		AutoInit:    argBool(args, "auto_init"),
	})
	if err != nil {
		return extension.ToolResultData{}, errFailed("create %s: %v", slug, err)
	}
	return extension.ToolResultData{
		Content: fmt.Sprintf("created %s (default branch: %s)", r.FullName, r.DefaultBranch),
		Data:    map[string]any{"repo": r},
	}, nil
}

func (t *toolset) repoRename(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	c, repo, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	newName, err := required(args, "name")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	r, err := c.RenameRepo(ctx, newName)
	if err != nil {
		return extension.ToolResultData{}, errFailed("rename %s: %v", repo, err)
	}
	return extension.ToolResultData{
		Content: fmt.Sprintf("renamed %s -> %s", repo, r.FullName),
		Data:    map[string]any{"repo": r},
	}, nil
}

func (t *toolset) repoDelete(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	c, repo, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	if err := t.requireRepo(ctx, PermRepoDelete, repo); err != nil {
		return extension.ToolResultData{}, err
	}

	// Check the scope BEFORE calling: GitHub answers 403 for both "no permission" and "not
	// found" on DELETE, so a missing scope would read as "already gone".
	if _, sc, ierr := c.IdentityAndScopes(ctx); ierr == nil {
		if cerr := ghclient.CheckTokenCanDelete(sc); cerr != nil {
			return extension.ToolResultData{}, errArg(cerr.Error())
		}
	}

	// The caller must spell the repository out. Deleting is irreversible and the usual failure
	// is a wrong slug, not a wrong decision.
	if confirm := argStr(args, "confirm"); confirm != repo {
		return extension.ToolResultData{}, errArg(fmt.Sprintf(
			"refusing to delete %s: pass confirm=%q to confirm", repo, repo))
	}
	if err := c.DeleteRepo(ctx); err != nil {
		return extension.ToolResultData{}, errFailed("delete %s: %v", repo, err)
	}
	return extension.ToolResultData{Content: "deleted " + repo}, nil
}

// shortSHA abbreviates a commit sha for display. Tool output is read by a model and a human;
// both recognise 7 characters and neither benefits from 40.
func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}
