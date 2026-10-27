package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/easydev-org/easybus-sdk/extension"

	"github.com/easydev-org/easyagent-github-ext/internal/workspace"
)

// Inspecting and undoing changes in a workspace.

// gitDiff shows what changed, as a unified diff.
func (t *toolset) gitDiff(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	Progress(ctx).Phase("staging for comparison")
	w, err := t.gws(args, session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	d, derr := w.DiffGoGitCtx(progressCtx(ctx), workspace.DiffOptions{
		Path:   argStr(args, "path"),
		Staged: argBool(args, "staged"),
	})
	if derr != nil {
		return extension.ToolResultData{}, errFailed("diff: %v", derr)
	}
	if strings.TrimSpace(d) == "" {
		return extension.ToolResultData{Content: "no changes", Data: map[string]any{
			"view": viewFor(w.ID, wsRoute(w.ID, "/changes"), "Changes"),
		}}, nil
	}

	// Capped: a large change would drown the turn, and the cap is stated rather than silent.
	const maxLines = 2000
	lines := strings.Split(d, "\n")
	truncated := len(lines) > maxLines
	if truncated {
		lines = lines[:maxLines]
	}
	out := strings.Join(lines, "\n")
	if truncated {
		out += fmt.Sprintf("\n\n... [truncated at %d of %d lines; pass `path` to narrow it]",
			maxLines, len(strings.Split(d, "\n")))
	}
	return extension.ToolResultData{
		Content: out,
		Data: map[string]any{
			"truncated": truncated,
			"view":      viewFor(w.ID, wsRoute(w.ID, "/changes"), "Changes"),
		},
	}, nil
}

// gitBlame reports who last changed each line of a file.
func (t *toolset) gitBlame(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	Progress(ctx).Phase("walking history for each line")
	w, err := t.gws(args, session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	path, err := required(args, "path")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	lines, berr := w.Blame(path, argInt(args, "from"), argInt(args, "to"))
	if berr != nil {
		return extension.ToolResultData{}, errArg(berr.Error())
	}
	if len(lines) == 0 {
		return extension.ToolResultData{Content: "no lines in that range"}, nil
	}
	var b strings.Builder
	for _, l := range lines {
		fmt.Fprintf(&b, "%4d %s %-16s %s  %s\n", l.Number, shortSHA(l.SHA), l.Author, l.When, l.Text)
	}
	fmt.Fprintf(&b, "\n%d lines", len(lines))
	return extension.ToolResultData{
		Content: b.String(),
		Data:    map[string]any{"lines": lines, "count": len(lines)},
	}, nil
}

// gitCommit records the workspace's changes locally. It does NOT push.
func (t *toolset) gitCommit(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	Progress(ctx).Phase("staging changes")
	w, err := t.gws(args, session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	msg, err := required(args, "message")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	changed, serr := w.Status()
	if serr != nil {
		return extension.ToolResultData{}, errFailed("status: %v", serr)
	}
	if len(changed) == 0 {
		return extension.ToolResultData{}, errArg("nothing to commit; the workspace is clean")
	}
	sha, cerr := w.Commit(workspace.CommitOptions{
		Message: msg,
		Author:  author(tenant).Name,
		Email:   author(tenant).Email,
	})
	if cerr != nil {
		return extension.ToolResultData{}, errFailed("commit: %v", cerr)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "committed %s\n", shortSHA(sha))
	fmt.Fprintf(&b, "%d file(s):\n", len(changed))
	for _, f := range changed {
		fmt.Fprintf(&b, "  %-9s %s\n", f.Status, f.Path)
	}
	b.WriteString("\nLocal only — publish with gh-pr-create.")
	return extension.ToolResultData{
		Content: b.String(),
		Data:    map[string]any{"commit": sha, "files": len(changed)},
	}, nil
}

// gitPull fast-forwards the workspace to its remote.
func (t *toolset) gitPull(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	Progress(ctx).Phase("fetching from the remote")
	w, err := t.gws(args, session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	old, new, perr := w.Pull(workspace.PullOptions{
		Branch: argStr(args, "branch"),
		Token:  t.token(tenant),
	})
	if perr != nil {
		return extension.ToolResultData{}, errFailed("%v", perr)
	}
	if old == new {
		return extension.ToolResultData{
			Content: fmt.Sprintf("already up to date (%s)", shortSHA(old)),
			Data:    map[string]any{"changed": false, "sha": old},
		}, nil
	}
	return extension.ToolResultData{
		Content: fmt.Sprintf("fast-forwarded %s -> %s", shortSHA(old), shortSHA(new)),
		Data:    map[string]any{"changed": true, "from": old, "to": new},
	}, nil
}

// gitReset discards working-tree changes.
func (t *toolset) gitReset(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	Progress(ctx).Phase("restoring files")
	w, err := t.gws(args, session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	path := argStr(args, "path")
	hard := argBool(args, "hard")
	// Confirm is required for a hard reset of everything: it deletes untracked files, which is
	// the one operation here that can destroy work that was never committed.
	if hard && path == "" && !argBool(args, "confirm") {
		return extension.ToolResultData{}, errArg(
			"a hard reset of the whole workspace deletes untracked files and cannot be undone; " +
				"pass confirm=true, or narrow it with `path`")
	}
	n, rerr := w.Reset(workspace.ResetOptions{Path: path, Hard: hard})
	if rerr != nil {
		return extension.ToolResultData{}, errFailed("reset: %v", rerr)
	}
	what := "restored"
	if hard {
		what = "discarded (including untracked)"
	}
	return extension.ToolResultData{
		Content: fmt.Sprintf("%s %d path(s)", what, n),
		Data:    map[string]any{"reset": n, "hard": hard},
	}, nil
}
