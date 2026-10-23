package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/easydev-org/easyagent-gitea-ext/internal/stage"
	easybus "github.com/easydev-org/easybus-sdk"
	"github.com/easydev-org/easybus-sdk/extension"
)

// Stage tools: read and edit repository content WITHOUT a clone and WITHOUT a sandbox.
//
// The existing git-* tools run git inside a sandbox, which is right for "build this, run
// the tests". It is entirely wrong for "change one line and open a PR": spawning a
// container to send three API calls makes a one-second edit take a minute, and it makes
// the change invisible until the container exists.
//
// These tools work the way a reviewer thinks — read a file, change it, look at the diff,
// submit — against content stored in a database.

// stageFor returns the staging area for this call's session and repository.
func (t *toolset) stageFor(ctx context.Context, session, tenant, repo, ref string) (*stage.Workspace, error) {
	if t.deps.Stage == nil {
		return nil, fmt.Errorf("staging is not configured on this extension")
	}
	return t.deps.Stage.Open(ctx, tenant, session, repo, ref)
}

// stageRepoArg accepts either a plain repository path or a configured remote name.
//
// The repositories a tenant may touch are the ones it already configured for the
// sandbox tools, so a name like "origin" or "app" resolves the same way there.
func (t *toolset) stageRepoArg(args map[string]any) string {
	repo := argStr(args, "repo")
	if repo == "" {
		return "default"
	}
	return repo
}

// stageTree lists every path visible in the staging area.
func (t *toolset) stageTree(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	w, err := t.stageFor(ctx, session, tenant, t.stageRepoArg(args), argStr(args, "ref"))
	if err != nil {
		return extension.ToolResultData{}, stageErr(err)
	}
	paths, err := w.Paths()
	if err != nil {
		return extension.ToolResultData{}, stageErr(err)
	}
	prefix := argStr(args, "path")
	if prefix != "" {
		kept := paths[:0]
		for _, p := range paths {
			if strings.HasPrefix(p, prefix) {
				kept = append(kept, p)
			}
		}
		paths = kept
	}
	return extension.ToolResultData{
		Content: strings.Join(paths, "\n"),
		Data:    map[string]any{"paths": paths, "base": w.Base(), "count": len(paths)},
	}, nil
}

// stageRead returns one file's content.
func (t *toolset) stageRead(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	path := argStr(args, "path")
	if path == "" {
		return extension.ToolResultData{}, argErr("path is required")
	}
	w, err := t.stageFor(ctx, session, tenant, t.stageRepoArg(args), argStr(args, "ref"))
	if err != nil {
		return extension.ToolResultData{}, stageErr(err)
	}
	content, ok, err := w.Read(path)
	if err != nil {
		return extension.ToolResultData{}, stageErr(err)
	}
	if !ok {
		return extension.ToolResultData{}, &extension.TypedError{
			Code:    easybus.ToolResultErrorCodeNotFound,
			Message: fmt.Sprintf("%s does not exist at the base revision", path),
		}
	}
	return extension.ToolResultData{
		Content: string(content),
		Data:    map[string]any{"path": path, "bytes": len(content)},
	}, nil
}

// stageWrite stages a new or replaced file.
func (t *toolset) stageWrite(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	path := argStr(args, "path")
	if path == "" {
		return extension.ToolResultData{}, argErr("path is required")
	}
	// NOT argStr: that trims, and a file's trailing newline is part of the file.
	content := argStrRaw(args, "content")
	w, err := t.stageFor(ctx, session, tenant, t.stageRepoArg(args), argStr(args, "ref"))
	if err != nil {
		return extension.ToolResultData{}, stageErr(err)
	}
	w.Write(path, []byte(content))
	return extension.ToolResultData{
		Content: fmt.Sprintf("staged %s (%d bytes); %s", path, len(content), t.stageHint(w)),
		Data:    map[string]any{"path": path, "bytes": len(content)},
	}, nil
}

// stageRemove stages a deletion.
func (t *toolset) stageRemove(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	path := argStr(args, "path")
	if path == "" {
		return extension.ToolResultData{}, argErr("path is required")
	}
	w, err := t.stageFor(ctx, session, tenant, t.stageRepoArg(args), argStr(args, "ref"))
	if err != nil {
		return extension.ToolResultData{}, stageErr(err)
	}
	w.Remove(path)
	return extension.ToolResultData{
		Content: fmt.Sprintf("staged deletion of %s; %s", path, t.stageHint(w)),
		Data:    map[string]any{"path": path, "deleted": true},
	}, nil
}

// stageStatus summarises what is staged.
func (t *toolset) stageStatus(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	w, err := t.stageFor(ctx, session, tenant, t.stageRepoArg(args), argStr(args, "ref"))
	if err != nil {
		return extension.ToolResultData{}, stageErr(err)
	}
	files, err := w.Status()
	if err != nil {
		return extension.ToolResultData{}, stageErr(err)
	}
	if len(files) == 0 {
		return extension.ToolResultData{
			Content: "nothing staged",
			Data:    map[string]any{"files": []any{}, "dirty": false},
		}, nil
	}
	var b strings.Builder
	for _, f := range files {
		mark := map[string]string{"added": "A", "modified": "M", "deleted": "D"}[f.Status]
		fmt.Fprintf(&b, "%s %s", mark, f.Path)
		if f.Binary {
			b.WriteString(" (binary)")
		} else if f.Additions > 0 || f.Deletions > 0 {
			fmt.Fprintf(&b, " (+%d -%d)", f.Additions, f.Deletions)
		}
		b.WriteString("\n")
	}
	return extension.ToolResultData{
		Content: strings.TrimRight(b.String(), "\n"),
		Data:    map[string]any{"files": files, "dirty": true},
	}, nil
}

// stageDiff renders the staged change as a unified diff.
func (t *toolset) stageDiff(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	w, err := t.stageFor(ctx, session, tenant, t.stageRepoArg(args), argStr(args, "ref"))
	if err != nil {
		return extension.ToolResultData{}, stageErr(err)
	}
	diff, err := w.Diff()
	if err != nil {
		return extension.ToolResultData{}, stageErr(err)
	}
	if strings.TrimSpace(diff) == "" {
		return extension.ToolResultData{Content: "no changes staged"}, nil
	}
	return extension.ToolResultData{
		Content: diff,
		Data:    map[string]any{"diff": diff},
	}, nil
}

// stageReset discards staged edits.
func (t *toolset) stageReset(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	w, err := t.stageFor(ctx, session, tenant, t.stageRepoArg(args), argStr(args, "ref"))
	if err != nil {
		return extension.ToolResultData{}, stageErr(err)
	}
	path := argStr(args, "path")
	n := w.ResetEdit(path)
	what := "all edits"
	if path != "" {
		what = path
	}
	return extension.ToolResultData{
		Content: fmt.Sprintf("discarded %d staged change(s) for %s", n, what),
		Data:    map[string]any{"discarded": n, "path": path},
	}, nil
}

// stageHint tells the caller what to do next, so a tool result is never a dead end.
func (t *toolset) stageHint(w *stage.Workspace) string {
	files, err := w.Status()
	if err != nil || len(files) == 0 {
		return "nothing else is staged"
	}
	return fmt.Sprintf("%d file(s) staged; review with git-stage-diff, then git-stage-submit", len(files))
}

// stageErr maps a staging failure onto a tool error a model can act on.
func stageErr(err error) error {
	return &extension.TypedError{
		// Absent state (no store, no revisions, nothing staged) is a Business error, not
		// an internal one: the caller can fix it by doing something else.
		Code:    easybus.ToolResultErrorCodeBusiness,
		Message: err.Error(),
	}
}

func argErr(msg string) error {
	return &extension.TypedError{Code: easybus.ToolResultErrorCodeInvalidArgument, Message: msg}
}
