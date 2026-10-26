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
	"strings"

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
	// PutObject stores a large object for a tenant. Used by gh-artifact-download: an artifact is
	// a zip of arbitrary size, so it is stored and referenced by name rather than returned in the
	// tool result. Nil means the deployment has no object storage, and that tool reports so.
	PutObject func(ctx context.Context, tenant, name string, data []byte) error
}

// Handler is one tool implementation.
type Handler func(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error)

// Permission keys this extension declares.
//
// THREE KEYS, not twelve. The granular ones answered "what kind of object is this?"; the question
// a reviewer actually decides is "is this session trusted with this repository, and how far?".
//
// The line between the two repository levels:
//
//	developer   may move the repository FORWARD — propose a change, discuss it, run CI
//	maintainer  may also move it BACKWARD — merge to the trunk, rewrite what was already
//	            published, delete the repository
//
// A maintainer grant also satisfies a developer check (see requireRepo); the reverse is false.
const (
	// PermDeveloper proposes and discusses: open issues, comment, open pull requests, run CI.
	// Scoped to one repository.
	PermDeveloper = "repo.developer"
	// PermMaintainer changes what is already published and the repository itself: merge, delete
	// a tag, delete or rename the repository.
	PermMaintainer = "repo.maintainer"
	// PermOrgAdmin creates repositories. A separate key rather than a repository permission
	// because the repository does not exist yet — there is no repo scope to grant it on, and
	// creating one is a decision about the ORGANIZATION, not about any existing repository.
	PermOrgAdmin = "org.admin"
	// NOTE: there is deliberately NO key for publishing a release. Releases are published by CI,
	// so no grant would make a hand-made one allowed; declaring a key would only invite somebody
	// to ask for one.
)

// requireMap names the permissions each tool needs.
//
// Only WRITE-class tools appear here. Read tools are deliberately ungated: a session that may
// not merge should still be able to LOOK at the pull request, and gating reads would make every
// grant also a grant of visibility.
var requireMap = map[string][]string{
	// Developer: propose, discuss, run CI.
	"gh-issue-create":  {PermDeveloper},
	"gh-issue-comment": {PermDeveloper},
	"gh-pr-create":     {PermDeveloper},
	"gh-run-dispatch":  {PermDeveloper},
	"gh-run-rerun":     {PermDeveloper},
	"gh-run-cancel":    {PermDeveloper},
	// Close is a developer action on your OWN work; requireOwnership adds that condition on top.
	"gh-issue-close": {PermDeveloper},

	// Maintainer: change what is already published, or the repository itself.
	"gh-pr-merge":    {PermMaintainer},
	"gh-tag-create":  {PermMaintainer},
	"gh-tag-delete":  {PermMaintainer},
	"gh-repo-delete": {PermMaintainer},
	"gh-repo-rename": {PermMaintainer},

	// Organization level: the repository does not exist yet.
	"gh-repo-create": {PermOrgAdmin},
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

		// Pull-request-only actions. Everything else about a pull request — its title,
		// body, comments, closing — is an ISSUE operation, because GitHub models a pull
		// request as a kind of issue sharing one numbering space. See items.go.
		"gh-pr-create": t.prCreate,
		"gh-pr-checks": t.prChecks,
		"gh-pr-merge":  t.prMerge,

		// What a pull request CHANGED. Without these, reviewing a PR means reading its title.
		"gh-pr-files": t.prFiles,
		"gh-pr-diff":  t.prDiff,

		// CI. gh-run-jobs is the live view; gh-run-log is available once a job finishes.
		"gh-workflow-list":     t.workflowList,
		"gh-run-list":          t.runList,
		"gh-run-jobs":          t.runJobs,
		"gh-run-log":           t.runLog,
		"gh-job-log":           t.jobLog,
		"gh-artifacts":         t.artifactList,
		"gh-artifact-download": t.artifactDownload,
		"gh-run-rerun":         t.runRerun,
		"gh-run-cancel":        t.runCancel,
		"gh-run-dispatch":      t.runDispatch,

		// ONE set of tools for issues AND pull requests: one number, one object, so a caller
		// need not know in advance which it holds.
		"gh-issue-create":   t.issueCreate,
		"gh-issue-list":     t.issueList,
		"gh-issue-show":     t.issueShow,
		"gh-issue-comment":  t.issueComment,
		"gh-issue-comments": t.issueComments,
		"gh-issue-close":    t.issueClose,
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
	target := map[string]string{"repo": repo}
	if extension.Allowed(ctx, perm, target) {
		return nil
	}
	// org.admin is scoped to an ORGANIZATION, not to a repository: creating "acme/new" asks about
	// the org named "acme". Checking it against {repo: ...} would never match, so a repository
	// creation would be impossible even with the grant.
	if perm == PermOrgAdmin {
		owner := repo
		if i := strings.IndexByte(repo, '/'); i > 0 {
			owner = repo[:i]
		}
		if extension.Allowed(ctx, PermOrgAdmin, map[string]string{"org": owner}) {
			return nil
		}
		return &extension.PermissionDenied{
			Key: PermOrgAdmin, Target: map[string]string{"org": owner},
			Granted: extension.GrantsFor(ctx, PermOrgAdmin),
		}
	}
	// LEVELS, one direction only: a maintainer is trusted with everything a developer is, so a
	// maintainer grant satisfies a developer check. The reverse would mean a developer could
	// merge, which is the distinction the two levels exist to draw.
	if perm == PermDeveloper && extension.Allowed(ctx, PermMaintainer, target) {
		return nil
	}
	return &extension.PermissionDenied{Key: perm, Target: target, Granted: extension.GrantsFor(ctx, perm)}
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
