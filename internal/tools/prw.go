package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/easydev-org/easybus-sdk/extension"
	"github.com/easylab-platform/easyvcs/store"

	"github.com/easydev-org/easyagent-github-ext/internal/stage"
)

// gh-stage-patch: the ONE tool that changes the staging area.
func (t *toolset) stagePatch(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	repo, err := required(args, "repo")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	patch, err := required(args, "patch")
	if err != nil {
		return extension.ToolResultData{}, err
	}

	w, err := t.openStage(ctx, tenant, session, repo, argStr(args, "base"))
	if err != nil {
		return extension.ToolResultData{}, err
	}
	ops, err := stage.ApplyPatch(w, patch)
	if err != nil {
		// The stage is untouched on failure, so the message can tell the caller to fix the
		// patch and retry without re-reading anything.
		return extension.ToolResultData{}, errArg(err.Error())
	}

	var b strings.Builder
	for _, op := range opts(ops) {
		fmt.Fprintf(&b, "%s %s\n", op, "")
	}
	paths, _ := w.Paths()
	fmt.Fprintf(&b, "\n%d file(s) staged; %d visible in the repository", len(ops), len(paths))
	return extension.ToolResultData{
		Content: b.String(),
		Data:    map[string]any{"applied": len(ops)},
	}, nil
}

// opts renders the applied operations for the summary line.
func opts(ops []stage.PatchOp) []string {
	out := make([]string, 0, len(ops))
	for _, o := range ops {
		verb := map[string]string{"add": "created", "update": "updated", "delete": "deleted"}[o.Kind]
		out = append(out, fmt.Sprintf("%-8s %s", verb, o.Path))
	}
	return out
}

func (t *toolset) stageStatus(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	repo, err := required(args, "repo")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	w := t.deps.Stage.For(tenant, session, repo)
	if w == nil {
		return extension.ToolResultData{Content: "nothing staged for this session and repository"}, nil
	}
	files, err := w.Status()
	if err != nil {
		return extension.ToolResultData{}, errFailed("status: %v", err)
	}
	if len(files) == 0 {
		return extension.ToolResultData{Content: "nothing staged"}, nil
	}
	var b strings.Builder
	for _, f := range files {
		fmt.Fprintf(&b, "%-8s %s\n", f.Status, f.Path)
	}
	fmt.Fprintf(&b, "\n%d file(s) staged", len(files))
	return extension.ToolResultData{
		Content: b.String(),
		Data:    map[string]any{"files": files},
	}, nil
}

func (t *toolset) stageDiff(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	repo, err := required(args, "repo")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	w := t.deps.Stage.For(tenant, session, repo)
	if w == nil {
		return extension.ToolResultData{Content: "nothing staged for this session and repository"}, nil
	}
	d, err := w.Diff()
	if err != nil {
		return extension.ToolResultData{}, errFailed("diff: %v", err)
	}
	if strings.TrimSpace(d) == "" {
		return extension.ToolResultData{Content: "nothing staged"}, nil
	}
	return extension.ToolResultData{Content: d}, nil
}

func (t *toolset) stageReset(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	repo, err := required(args, "repo")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	w := t.deps.Stage.For(tenant, session, repo)
	if w == nil {
		return extension.ToolResultData{Content: "nothing staged"}, nil
	}
	n := w.ResetEdit(argStr(args, "path"))
	return extension.ToolResultData{
		Content: fmt.Sprintf("cleared %d staged edit(s)", n),
		Data:    map[string]any{"cleared": n},
	}, nil
}

// openStage returns the staging area for this session and repository, creating it if needed.
//
// The base is imported from GitHub on first use: the staging area reads stored content, so
// there has to be stored content. Doing it here rather than as a separate caller step means a
// session cannot end up editing a stage whose base was never fetched.
func (t *toolset) openStage(ctx context.Context, tenant, session, repo, base string) (*stage.Workspace, error) {
	if w := t.deps.Stage.For(tenant, session, repo); w != nil {
		return w, nil
	}
	remote, err := remoteFor(repo)
	if err != nil {
		return nil, err
	}
	if base == "" {
		// Import the DEFAULT branch: that is what a pull request would target.
		if _, err := t.deps.Stage.Import(ctx, stage.ImportParams{
			Tenant: tenant, Dest: remote, Branch: "", Token: t.token(tenant),
		}); err != nil {
			return nil, errFailed("import %s: %v", repo, err)
		}
	} else {
		if _, err := t.deps.Stage.Import(ctx, stage.ImportParams{
			Tenant: tenant, Dest: remote, Branch: base, Token: t.token(tenant),
		}); err != nil {
			return nil, errFailed("import %s@%s: %v", repo, base, err)
		}
	}
	return t.deps.Stage.Open(ctx, tenant, session, repo, "")
}

// gh-pr-create: the ONLY way a change reaches GitHub.
//
// Staging area → one commit on a fresh branch → pull request. There is no commit tool and no
// push tool, so this sequence cannot be split or bypassed.
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
	w := t.deps.Stage.For(tenant, session, repo)
	if w == nil || !w.Dirty() {
		return extension.ToolResultData{}, errArg("nothing staged; use gh-stage-patch first")
	}

	base := argStr(args, "base")
	if base == "" {
		// The repository's own default branch, not a guess: a PR against the wrong base is a
		// change to the wrong branch.
		c, _, cerr := t.clientFor(tenant, args)
		if cerr != nil {
			return extension.ToolResultData{}, cerr
		}
		info, ierr := c.Info(ctx)
		if ierr != nil {
			return extension.ToolResultData{}, errFailed("resolve default branch: %v", ierr)
		}
		base = info.DefaultBranch
	}

	branch, err := newBranchName()
	if err != nil {
		return extension.ToolResultData{}, errFailed("branch name: %v", err)
	}

	// The staged content, as plain files: the commit is built from these through GitHub's
	// Git Data API so it descends from `base` (see CommitFiles for why the gitbridge export
	// path cannot be used here).
	files, deletes, err := stagedFiles(w)
	if err != nil {
		return extension.ToolResultData{}, errFailed("read stage: %v", err)
	}
	if len(files) == 0 && len(deletes) == 0 {
		return extension.ToolResultData{}, errArg("nothing staged; use gh-stage-patch first")
	}

	c, _, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	if _, err := c.CommitFiles(ctx, branch, base, title, files, deletes); err != nil {
		return extension.ToolResultData{}, errFailed("commit %s: %v", branch, err)
	}

	body := argStr(args, "body")
	pr, err := c.CreatePR(ctx, base, branch, title, body)
	if err != nil {
		// The branch exists but no PR does. Say so, including the branch name: the caller can
		// retry the PR without redoing the work, and a silent failure would leave an orphaned
		// branch nobody knows about.
		return extension.ToolResultData{}, errFailed(
			"created branch %s but could not open the pull request: %v (retry with the same branch)",
			branch, err)
	}
	// Only now is the stage safe to clear: everything it held is on the branch.
	w.ResetEdit("")

	var b strings.Builder
	fmt.Fprintf(&b, "opened pull request #%d\n", pr.Number)
	fmt.Fprintf(&b, "%s\n", pr.HTMLURL)
	fmt.Fprintf(&b, "branch: %s → %s\n", branch, base)
	fmt.Fprintf(&b, "%d file(s)\n", len(files)+len(deletes))
	for p := range files {
		fmt.Fprintf(&b, "  %-8s %s\n", "write", p)
	}
	for _, p := range deletes {
		fmt.Fprintf(&b, "  %-8s %s\n", "delete", p)
	}
	b.WriteString("\nCI runs on the PR; check it with gh-pr-checks.")
	return extension.ToolResultData{
		Content: b.String(),
		Data: map[string]any{
			"pr": pr.Number, "url": pr.HTMLURL, "branch": branch, "base": base,
			"files": len(files) + len(deletes),
		},
	}, nil
}

// stagedFiles reads the stage into what the Git Data API needs: path -> content, and the paths
// to delete.
func stagedFiles(w *stage.Workspace) (map[string]string, []string, error) {
	files := map[string]string{}
	var deletes []string
	edits := w.Edits()
	for _, e := range edits {
		if e.Delete {
			deletes = append(deletes, e.Path)
			continue
		}
		files[e.Path] = string(e.Content)
	}
	return files, deletes, nil
}

// newBranchName makes a fresh branch name every time.
//
// Never reused: a second change that reused `pr/<id>` would rewrite the branch a reviewer is
// already looking at, turning their review into a moving target.
func newBranchName() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "pr/" + hex.EncodeToString(b[:]), nil
}

// author is the commit identity.
func author(tenant string) store.Author {
	return store.Author{Name: "easyagent", Email: "easyagent@" + tenant + ".local"}
}
