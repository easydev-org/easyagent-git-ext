// Package tools implements the gh-* tool handlers.
//
// One rule shapes this package: the ONLY way to change a repository is gh-pr-create, which
// turns the staging area into a branch and a pull request. There is no commit tool, no push
// tool, and no tool that writes to a branch. A caller cannot accidentally take a shortcut
// because there is no shortcut to take.
package tools

import (
	"context"
	"fmt"

	easybus "github.com/easydev-org/easybus-sdk"
	"github.com/easydev-org/easybus-sdk/extension"

	"github.com/easydev-org/easyagent-github-ext/internal/config"
	"github.com/easydev-org/easyagent-github-ext/internal/i18n"
	"github.com/easydev-org/easyagent-github-ext/internal/stage"
)

// Deps is what the tools need from the extension host.
type Deps struct {
	GetConfig func(name, session, tenant string) any
	Locale    func(session, tenant string) string
	// Stage is the staging area. It is required: without it the extension has no way to hold
	// edits between tool calls, and every write tool would be meaningless.
	Stage *stage.Stage
}

// Handler is one tool implementation.
type Handler func(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error)

// Permission keys this extension declares.
const (
	// PermPRCreate opens pull requests. Scoped to a repository.
	PermPRCreate = "pr.create"
	// PermPRMerge merges pull requests. Destructive, because it changes the base branch.
	PermPRMerge = "pr.merge"
	// PermPRClose closes pull requests without merging.
	PermPRClose = "pr.close"
	// PermRepoCreate creates a repository.
	PermRepoCreate = "repo.create"
	// PermRepoDelete deletes a repository. Destructive and irreversible.
	PermRepoDelete = "repo.delete"
	// PermTagCreate creates tags — the event a downstream subscription reacts to.
	PermTagCreate = "tag.create"
	// PermTagDelete deletes a tag. Destructive: a released version disappears with it.
	PermTagDelete = "tag.delete"
	// NOTE: there is deliberately NO release.create permission. Releases are published by CI,
	// not by a caller, so no grant would make the operation allowed — declaring a key would only
	// invite somebody to ask for one.
)

// requireMap names the permissions each tool needs.
//
// Only WRITE-class tools appear here. Read tools are deliberately ungated: a session that may
// not merge should still be able to LOOK at the pull request, and gating reads would make every
// grant also a grant of visibility.
var requireMap = map[string][]string{
	"gh-pr-create":   {PermPRCreate},
	"gh-pr-merge":    {PermPRMerge},
	"gh-pr-close":    {PermPRClose},
	"gh-repo-create": {PermRepoCreate},
	"gh-repo-delete": {PermRepoDelete},
	"gh-tag-create":  {PermTagCreate},
	"gh-tag-delete":  {PermTagDelete},
}

// RequireMap returns a copy so a caller cannot mutate the shared table.
func RequireMap() map[string][]string {
	out := make(map[string][]string, len(requireMap))
	for k, v := range requireMap {
		out[k] = append([]string(nil), v...)
	}
	return out
}

// Registry builds the gh-* handler map.
func Registry(d Deps) map[string]Handler {
	t := &toolset{deps: d}
	return map[string]Handler{
		// Reading. No token needed for a public repository.
		"gh-info": t.info,
		"gh-tree": t.tree,
		"gh-read": t.read,

		// Account.
		"gh-whoami":   t.whoami,
		"gh-orgs":     t.orgs,
		"gh-repos":    t.repos,
		"gh-releases": t.releases,
		"gh-packages": t.packages,

		// Publishing. gh-tag-create is the event a downstream subscription waits for.
		"gh-tag-create": t.tagCreate,
		"gh-tag-list":   t.tagList,
		"gh-tag-show":   t.tagShow,
		"gh-tag-delete": t.tagDelete,

		// Repository administration.
		"gh-repo-create": t.repoCreate,
		"gh-repo-rename": t.repoRename,
		"gh-repo-delete": t.repoDelete,

		// The staging area. ONE editing tool.
		"gh-stage-patch":  t.stagePatch,
		"gh-stage-status": t.stageStatus,
		"gh-stage-diff":   t.stageDiff,
		"gh-stage-reset":  t.stageReset,

		// The only way out: a pull request.
		"gh-pr-create": t.prCreate,
		"gh-pr-list":   t.prList,
		"gh-pr-show":   t.prShow,
		"gh-pr-checks": t.prChecks,
		"gh-pr-merge":  t.prMerge,
		"gh-pr-close":  t.prClose,
	}
}

// toolset carries the dependencies through every handler.
type toolset struct {
	deps Deps
}

// requireRepo checks a permission against the repository a call targets.
//
// The repository is taken from the caller's arguments, not from configuration: what a session
// may do to `someone/elsewhere` is a different authorisation from what it may do here, and
// deriving it any other way would let a caller name one repository and be authorised for
// another.
func (t *toolset) requireRepo(ctx context.Context, perm string, repo string) error {
	if repo == "" {
		return &extension.TypedError{
			Code:    easybus.ToolResultErrorCodeInvalidArgument,
			Message: "a repository (owner/name) is required",
		}
	}
	return extension.Require(ctx, perm, map[string]string{"repo": repo})
}

// errArg builds a consistent invalid-argument error.
func errArg(msg string) error {
	return &extension.TypedError{Code: easybus.ToolResultErrorCodeInvalidArgument, Message: msg}
}

// errFailed builds a consistent failure error.
func errFailed(format string, a ...any) error {
	return &extension.TypedError{Code: easybus.ToolResultErrorCodeRetryable, Message: fmt.Sprintf(format, a...)}
}

// tr looks up a message for the caller's locale.
func (t *toolset) tr(session, tenant, key string, params map[string]any) string {
	loc := "en"
	if t.deps.Locale != nil {
		loc = t.deps.Locale(session, tenant)
	}
	return i18n.T(loc, key, params)
}

var _ = config.KnobToken
