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
	"github.com/easydev-org/easyagent-github-ext/internal/workspace"
)

// easybusInternal is the SDK's internal-error code, aliased so a handler can say "the
// deployment is misconfigured" without importing the SDK for one constant.
const easybusInternal = easybus.ToolResultErrorCodeInternal

// CloneParams is the subset of workspace.CloneOptions the tool layer fills in.
//
// Declared here so the tools package does not have to know the shape of the workspace package's
// options struct — the mapping lives in one place, in gitlocal.go.
type CloneParams = workspace.CloneOptions

// Deps is what the tools need from the extension host.
type Deps struct {
	GetConfig func(name, session, tenant string) any
	Locale    func(session, tenant string) string
	// Workspaces holds the per-session git checkouts. Required: without it no git-* tool can
	// run, because every one of them operates on a workspace.
	Workspaces *workspace.Manager
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
		// LOCAL git. Everything here happens inside a workspace directory; none of it talks to
		// GitHub. A workspace comes from git-ws-clone and is identified by its id.
		"git-ws-clone":     t.gitClone,
		"git-ws-workspace": t.gitWorkspace,
		"git-ws-status":    t.gitStatus,
		"git-ws-patch":     t.gitPatch,
		"git-ws-read":      t.gitRead,
		"git-ws-list":      t.gitList,
		"git-ws-log":       t.gitLog,
		"git-ws-diff":      t.gitDiff,
		"git-ws-blame":     t.gitBlame,
		"git-ws-commit":    t.gitCommit,
		"git-ws-pull":      t.gitPull,
		"git-ws-reset":     t.gitReset,

		// GITHUB PLATFORM. Repository metadata, account, releases, packages.
		"gh-info":     t.info,
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

		// The only way out: a pull request, built from a workspace branch.
		"gh-pr-create": t.prCreate,
		"gh-pr-list":   t.prList,
		"gh-pr-show":   t.prShow,
		"gh-pr-checks": t.prChecks,
		"gh-pr-merge":  t.prMerge,
		"gh-pr-close":  t.prClose,

		// What a pull request CHANGED. Without these, reviewing a PR means reading its title.
		"gh-pr-files": t.prFiles,
		"gh-pr-diff":  t.prDiff,

		// CI. gh-run-jobs is the live view; gh-run-log is available once a job finishes.
		"gh-workflow-list": t.workflowList,
		"gh-run-list":      t.runList,
		"gh-run-jobs":      t.runJobs,
		"gh-run-log":       t.runLog,
		"gh-job-log":       t.jobLog,
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
