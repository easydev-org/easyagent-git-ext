package tools

import (
	"context"
	"fmt"

	"github.com/easydev-org/easyagent-gitea-ext/internal/config"
	"github.com/easydev-org/easyagent-gitea-ext/internal/stage"
	easybus "github.com/easydev-org/easybus-sdk"
	"github.com/easydev-org/easybus-sdk/extension"
)

// stageImport handles `git-stage-import`: bring a remote repository's content into the
// staging store so it can be read and edited without a checkout.
//
// This is the step that makes the staging area usable from nothing. Until a repository's
// content is in the store, there is no base revision to read from — the tools can list
// an empty tree but nothing else.
func (t *toolset) stageImport(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	cfg := t.cfg(session, tenant)

	remote := argStr(args, "remote")
	if remote == "" {
		return extension.ToolResultData{}, &extension.TypedError{
			Code:    easybus.ToolResultErrorCodeInvalidArgument,
			Message: "remote is required, e.g. https://git.example.com/owner/repo.git",
		}
	}
	branch := argStr(args, "branch")
	if branch == "" {
		branch = cfg.DefaultBranch
	}
	if branch == "" {
		branch = "main"
	}

	// The `servers` entry supplies the credential for a private repository; a public one
	// imports anonymously.
	srv, _ := cfg.ServerForHost(config.HostOf(remote))

	st := t.deps.Stage
	if st == nil {
		return extension.ToolResultData{}, &extension.TypedError{
			Code:    easybus.ToolResultErrorCodeBusiness,
			Message: "staging is not configured on this extension",
		}
	}

	res, err := st.Import(ctx, stage.ImportParams{
		Tenant: tenant,
		Dest:   remote,
		Branch: branch,
		Token:  srv.Token,
	})
	if err != nil {
		return extension.ToolResultData{}, &extension.TypedError{
			Code:    easybus.ToolResultErrorCodeBusiness,
			Message: fmt.Sprintf("import %s: %v", remote, err),
		}
	}

	msg := fmt.Sprintf("imported %s@%s into the staging store (%d revision(s))",
		remote, branch, res.Revisions)
	if res.Revisions == 0 {
		// Importing an already-imported branch is a no-op, not a failure; saying "0"
		// without context would read like nothing happened.
		msg = fmt.Sprintf("%s@%s is already imported (%d revision(s) in the store)",
			remote, branch, res.Total)
	}
	return extension.ToolResultData{
		Content: msg,
		Data: map[string]any{
			"remote": remote, "branch": branch,
			"revisions": res.Revisions, "total": res.Total,
		},
	}, nil
}
