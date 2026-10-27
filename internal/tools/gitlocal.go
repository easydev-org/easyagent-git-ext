package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/easydev-org/easybus-sdk/extension"

	"github.com/easydev-org/easyagent-github-ext/internal/workspace"
)

// LOCAL git tools. They work inside a workspace directory and never touch GitHub.
//
// The split matters: `git-*` is the repository on disk, `gh-*` is the GitHub platform. A caller
// reading a file is doing the first; asking about pull requests is the second.

// gws resolves the workspace named in the arguments, checking the caller owns it.
func (t *toolset) gws(args map[string]any, session, tenant string) (*workspace.Workspace, error) {
	if t.deps.Workspaces == nil {
		return nil, &extension.TypedError{
			Code:    easybusInternal,
			Message: "this deployment has no workspace support",
		}
	}
	id, err := required(args, "workspace")
	if err != nil {
		return nil, err
	}
	w, gerr := t.deps.Workspaces.Get(id, tenant, session)
	if gerr != nil {
		if gerr == workspace.ErrNoWorkspace {
			return nil, errArg("no workspace with id " + id +
				"; create one with git-ws-clone (it returns the id)")
		}
		if gerr == workspace.ErrNotOwned {
			// Deliberately the same message as "not found": confirming that an id EXISTS under
			// another session would leak that somebody else is working on this repository.
			return nil, errArg("no workspace with id " + id)
		}
		return nil, errFailed("workspace: %v", gerr)
	}
	return w, nil
}

// gitClone creates a workspace. This is the ONLY way to get a workspace id.
func (t *toolset) gitClone(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	if t.deps.Workspaces == nil {
		return extension.ToolResultData{}, &extension.TypedError{
			Code: easybusInternal, Message: "this deployment has no workspace support",
		}
	}
	repo, err := required(args, "repo")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	remote, rerr := remoteFor(repo)
	if rerr != nil {
		return extension.ToolResultData{}, rerr
	}
	w, cerr := t.deps.Workspaces.CloneCtx(progressCtx(ctx), CloneParams{
		Tenant: tenant, Session: session, Repo: repo, URL: remote,
		Ref: argStr(args, "ref"), Token: t.token(tenant), Depth: argInt(args, "depth"),
	})
	if cerr != nil {
		return extension.ToolResultData{}, errFailed("%s", cerr.Error())
	}

	var b strings.Builder
	fmt.Fprintf(&b, "workspace: %s\n", w.ID)
	fmt.Fprintf(&b, "repo: %s\n", w.Repo)
	if br := argStr(args, "ref"); br != "" {
		fmt.Fprintf(&b, "ref: %s\n", br)
	}
	files, _ := w.ListFiles()
	fmt.Fprintf(&b, "files: %d\n", len(files))
	b.WriteString("\nPass this id as `workspace` to every other git-* tool.")
	return extension.ToolResultData{
		Content: b.String(),
		Data: map[string]any{
			"workspace": w.ID,
			"repo":      w.Repo,
			"files":     len(files),
			// The id exists only as a result here — no argument and no manifest can name it — so the
			// view route is built by the code that just created it.
			"view": viewFor(w.ID, wsRoute(w.ID, "/tree/"), w.Repo),
		},
	}, nil
}

// gitWorkspace lists or closes workspaces.
func (t *toolset) gitWorkspace(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	Progress(ctx).Phase("listing workspaces")
	if t.deps.Workspaces == nil {
		return extension.ToolResultData{}, &extension.TypedError{
			Code: easybusInternal, Message: "this deployment has no workspace support",
		}
	}
	if closeID := argStr(args, "close"); closeID != "" {
		if err := t.deps.Workspaces.Close(closeID, tenant, session); err != nil {
			return extension.ToolResultData{}, errArg("close: " + err.Error())
		}
		return extension.ToolResultData{Content: "closed workspace " + closeID}, nil
	}
	list := t.deps.Workspaces.List(tenant, session)
	if len(list) == 0 {
		return extension.ToolResultData{Content: "no workspaces; create one with git-ws-clone"}, nil
	}
	var b strings.Builder
	for _, w := range list {
		fmt.Fprintf(&b, "%s  %-32s created %s\n", w.ID, w.Repo, w.CreatedAt.Format("15:04:05"))
	}
	fmt.Fprintf(&b, "\n%d workspaces", len(list))
	return extension.ToolResultData{
		Content: b.String(),
		Data:    map[string]any{"workspaces": list, "count": len(list)},
	}, nil
}

// gitStatus reports what changed in the workspace.
func (t *toolset) gitStatus(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	Progress(ctx).Phase("reading the working tree")
	w, err := t.gws(args, session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	files, serr := w.Status()
	if serr != nil {
		return extension.ToolResultData{}, errFailed("status: %v", serr)
	}
	if len(files) == 0 {
		return extension.ToolResultData{Content: "clean (nothing changed)", Data: map[string]any{
			"files": files,
			// Still a view: the Changes pane is where a caller checks, and an empty one says so.
			"view": viewFor(w.ID, wsRoute(w.ID, "/changes"), "Changes"),
		}}, nil
	}
	var b strings.Builder
	for _, f := range files {
		mark := " "
		if f.Staged {
			mark = "+"
		}
		fmt.Fprintf(&b, "%s %-9s %s\n", mark, f.Status, f.Path)
	}
	fmt.Fprintf(&b, "\n%d changed", len(files))
	return extension.ToolResultData{
		Content: b.String(),
		Data: map[string]any{
			"files": files, "count": len(files),
			"view": viewFor(w.ID, wsRoute(w.ID, "/changes"), "Changes"),
		},
	}, nil
}

// gitPatch edits files in the workspace — the ONLY editing tool.
func (t *toolset) gitPatch(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	Progress(ctx).Phase("reading current files")
	w, err := t.gws(args, session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	patch, err := required(args, "patch")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	ops, aerr := w.ApplyPatch(patch)
	if aerr != nil {
		// Nothing was written on failure, so the caller can fix the patch and retry.
		return extension.ToolResultData{}, errArg(aerr.Error())
	}
	var b strings.Builder
	for _, op := range ops {
		verb := map[string]string{"add": "created", "update": "updated", "delete": "deleted"}[op.Kind]
		fmt.Fprintf(&b, "%-8s %s\n", verb, op.Path)
	}
	fmt.Fprintf(&b, "\n%d file(s) changed", len(ops))
	return extension.ToolResultData{
		Content: b.String(),
		Data:    map[string]any{"applied": len(ops)},
	}, nil
}

// gitRead reads a file from the workspace, with line numbers.
func (t *toolset) gitRead(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	Progress(ctx).Phase("reading the file")
	w, err := t.gws(args, session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	path, err := required(args, "path")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	content, rerr := w.ReadFile(path)
	if rerr != nil {
		return extension.ToolResultData{}, errArg(rerr.Error())
	}
	return extension.ToolResultData{
		Content: numberLines(string(content)),
		Data: map[string]any{
			"path": path, "bytes": len(content),
			// A read names one file, so the view is that file rather than the tree.
			"view": viewFor(w.ID, wsRoute(w.ID, "/blob/"+escapePath(path)), path),
		},
	}, nil
}

// gitList lists files in the workspace.
func (t *toolset) gitList(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	Progress(ctx).Phase("walking the working tree")
	w, err := t.gws(args, session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	files, lerr := w.ListFiles()
	if lerr != nil {
		return extension.ToolResultData{}, errFailed("list: %v", lerr)
	}
	var b strings.Builder
	for _, f := range files {
		b.WriteString(f)
		b.WriteString("\n")
	}
	if len(files) == 0 {
		b.WriteString("(no files)\n")
	}
	fmt.Fprintf(&b, "\n%d files", len(files))
	return extension.ToolResultData{
		Content: b.String(),
		Data: map[string]any{
			"files": files, "count": len(files),
			"view": viewFor(w.ID, wsRoute(w.ID, "/tree/"), "Files"),
		},
	}, nil
}

// gitLog shows the workspace's real commit history.
func (t *toolset) gitLog(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	Progress(ctx).Phase("reading history")
	w, err := t.gws(args, session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	entries, lerr := w.Log(argInt(args, "limit"))
	if lerr != nil {
		return extension.ToolResultData{}, errFailed("log: %v", lerr)
	}
	var b strings.Builder
	for _, e := range entries {
		first, _, _ := strings.Cut(e.Message, "\n")
		fmt.Fprintf(&b, "%s  %-20s %s  %s\n", shortSHA(e.SHA), e.Author, e.When, first)
	}
	if len(entries) == 0 {
		b.WriteString("(no commits)\n")
	}
	fmt.Fprintf(&b, "\n%d commits", len(entries))
	return extension.ToolResultData{
		Content: b.String(),
		Data: map[string]any{
			"log": entries, "count": len(entries),
			"view": viewFor(w.ID, wsRoute(w.ID, "/log"), "History"),
		},
	}, nil
}
