// Package tools implements the git-* tool handlers. Every handler resolves the
// calling tenant's config, selects the named sandbox, verifies the target path
// is a git repository, and runs git there via the worker's Execute RPC — git
// runs IN the sandbox, so no system git is needed here.
package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/easydev-org/easyagent-gitea-ext/internal/config"
	"github.com/easydev-org/easyagent-gitea-ext/internal/i18n"
	"github.com/easydev-org/easyagent-gitea-ext/internal/sandbox"
	"github.com/easydev-org/easyagent-gitea-ext/internal/stage"
	easybus "github.com/easydev-org/easybus-sdk"
	"github.com/easydev-org/easybus-sdk/extension"
)

// Deps is what the tools need from the extension host.
type Deps struct {
	GetConfig func(name, session, tenant string) any
	Locale    func(session, tenant string) string
	Cache     *sandbox.ClientCache
	// Stage, when set, enables the git-stage-* tools: they read and edit repository
	// content WITHOUT a clone or a sandbox.
	//
	// nil leaves them out entirely rather than failing at call time: a deployment that
	// has no easyvcs store should not advertise tools it cannot serve.
	Stage *stage.Stage
}

// Handler is one tool implementation.
type Handler func(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error)

// RequireMap names the permissions each tool needs.
//
// Separate from the handler map because a Handler is just a func: attaching the
// requirement to the func value would make it invisible to anything reading the manifest,
// and the two are consumed by different layers (this process and the agent).
//
// Only WRITE-class tools appear here. Read tools are deliberately ungated: a session that
// may not merge should still be able to LOOK at the pull request, and gating reads would
// make every permission grant also a grant of visibility.
var requireMap = map[string][]string{
	"git-mr-merge": {PermMRMerge},
	"git-mr-close": {PermMRClose},
}

// RequireMap returns a copy so a caller cannot mutate the shared table.
func RequireMap() map[string][]string {
	out := make(map[string][]string, len(requireMap))
	for k, v := range requireMap {
		out[k] = append([]string(nil), v...)
	}
	return out
}

// Registry builds the git-* handler map.
func Registry(d Deps) map[string]Handler {
	t := &toolset{deps: d}
	out := map[string]Handler{
		"git-clone":    t.clone,
		"git-status":   t.status,
		"git-log":      t.log,
		"git-diff":     t.diff,
		"git-branch":   t.branch,
		"git-checkout": t.checkout,
		"git-add":      t.add,
		"git-commit":   t.commit,
		"git-pull":     t.pull,
		// `git-push` is REFUSED, not removed: a caller that remembers the name is
		// told the workflow ("use git-submit-mr") instead of finding it gone.
		"git-push": t.refusePush,
		"git-show": t.show,
		// Review-request workflow (the only way changes reach the remote).
		"git-submit-mr":  t.submitMR,
		"git-mr-list":    t.listMRs,
		"git-mr-show":    t.mrShow,
		"git-mr-comment": t.mrComment,
		"git-mr-close":   t.mrClose,
		"git-mr-merge":   t.mrMerge,
		// Read-only remote tools: they talk to the Gitea/Forgejo API and need NO
		// sandbox and NO clone, so `path` does not apply to them.
		"git-remote-ls":   t.remoteLs,
		"git-remote-show": t.remoteShow,
	}

	// Stage tools need a backing store; without one the names would be dead ends.
	if d.Stage != nil {
		for name, h := range map[string]Handler{
			"git-tree":         t.stageTree,
			"git-read":         t.stageRead,
			"git-write":        t.stageWrite,
			"git-remove":       t.stageRemove,
			"git-stage-status": t.stageStatus,
			"git-stage-diff":   t.stageDiff,
			"git-stage-reset":  t.stageReset,
			"git-stage-submit": t.stageSubmit,
			"git-stage-import": t.stageImport,
		} {
			out[name] = h
		}
	}
	return out
}

type toolset struct{ deps Deps }

func (t *toolset) locale(session, tenant string) string {
	if t.deps.Locale != nil {
		return t.deps.Locale(session, tenant)
	}
	return ""
}

func (t *toolset) cfg(session, tenant string) config.Config {
	get := func(name string) string {
		if t.deps.GetConfig == nil {
			return ""
		}
		if s, ok := t.deps.GetConfig(name, session, tenant).(string); ok {
			return s
		}
		return ""
	}
	cfg, _ := config.Parse(get)
	return cfg
}

// runner resolves the sandbox named by args["sandbox"] and returns a Runner.
func (t *toolset) runner(args map[string]any, session, tenant string) (*sandbox.Runner, error) {
	cfg := t.cfg(session, tenant)
	wanted := argStr(args, "sandbox")
	sel, err := config.SelectSandbox(cfg.Sandboxes, wanted)
	if err != nil {
		return nil, &extension.TypedError{Code: easybus.ToolResultErrorCodeInvalidArgument, Message: err.Error()}
	}
	return sandbox.NewRunner(t.deps.Cache, sandbox.Endpoint{URL: sel.URL, Token: sel.Token}, sel.Name), nil
}

// repo resolves the runner AND verifies path is a git repository.
func (t *toolset) repo(ctx context.Context, args map[string]any, session, tenant string) (*sandbox.Runner, string, error) {
	r, err := t.runner(args, session, tenant)
	if err != nil {
		return nil, "", err
	}
	path := argStr(args, "path")
	if err := r.EnsureRepo(ctx, path); err != nil {
		return nil, "", err
	}
	return r, path, nil
}

// ---- handlers --------------------------------------------------------------

func (t *toolset) clone(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	r, err := t.runner(args, session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	remote := argStr(args, "remote")
	path := argStr(args, "path")
	if remote == "" || path == "" {
		return extension.ToolResultData{}, &extension.TypedError{Code: easybus.ToolResultErrorCodeInvalidArgument, Message: "remote and path are required"}
	}
	depth := argInt(args, "depth", 1)
	auth := t.authFor(session, tenant, remote)
	clone := "git clone " + shellQuote(remote) + " " + shellQuote(path)
	if depth > 0 {
		clone = "git clone --depth " + fmt.Sprintf("%d", depth) + " " + shellQuote(remote) + " " + shellQuote(path)
	}
	// The token never appears in `command` or `env` (both are observable in the
	// sandbox); it rides stdin only and is unset right after the clone.
	if auth.token != "" {
		clone = gitWithToken(clone, auth.user)
		if _, err := r.RunSecret(ctx, "", clone, auth.token, 300*time.Second); err != nil {
			return extension.ToolResultData{}, err
		}
	} else {
		if _, err := r.Run(ctx, "", clone, 300*time.Second); err != nil {
			return extension.ToolResultData{}, err
		}
	}
	head, _ := r.Git(ctx, path, 30*time.Second, "rev-parse", "--abbrev-ref", "HEAD")
	hash, _ := r.Git(ctx, path, 30*time.Second, "rev-parse", "--short", "HEAD")
	return extension.ToolResultData{
		Content: i18n.T(t.locale(session, tenant), i18n.MsgCloned, i18n.Params{"remote": remote, "branch": strings.TrimSpace(head), "hash": strings.TrimSpace(hash)}),
		Data:    map[string]any{"remote": remote, "path": path, "branch": strings.TrimSpace(head), "hash": strings.TrimSpace(hash)},
	}, nil
}

func (t *toolset) status(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	r, path, err := t.repo(ctx, args, session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	out, err := r.Git(ctx, path, 60*time.Second, "status", "--short", "--branch")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	return extension.ToolResultData{Content: strings.TrimSpace(out)}, nil
}

func (t *toolset) log(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	r, path, err := t.repo(ctx, args, session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	limit := argInt(args, "limit", 20)
	out, err := r.Git(ctx, path, 60*time.Second, "log", "-n", fmt.Sprintf("%d", limit), "--pretty=format:%h %s (%an, %ad)", "--date=short")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	return extension.ToolResultData{Content: strings.TrimSpace(out)}, nil
}

func (t *toolset) diff(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	r, path, err := t.repo(ctx, args, session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	gitArgs := []string{"diff"}
	if argBool(args, "staged", false) {
		gitArgs = append(gitArgs, "--cached")
	}
	if f := argStr(args, "from"); f != "" {
		gitArgs = append(gitArgs, shellQuote(f))
	}
	if t2 := argStr(args, "to"); t2 != "" {
		gitArgs = append(gitArgs, shellQuote(t2))
	}
	out, err := r.Git(ctx, path, 60*time.Second, gitArgs...)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	return extension.ToolResultData{Content: strings.TrimSpace(out)}, nil
}

func (t *toolset) branch(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	r, path, err := t.repo(ctx, args, session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	gitArgs := []string{"branch"}
	if argBool(args, "remoteOnly", false) {
		gitArgs = append(gitArgs, "-r")
	}
	out, err := r.Git(ctx, path, 60*time.Second, gitArgs...)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	return extension.ToolResultData{Content: strings.TrimSpace(out)}, nil
}

func (t *toolset) checkout(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	r, path, err := t.repo(ctx, args, session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	name := argStr(args, "branch")
	if name == "" {
		return extension.ToolResultData{}, &extension.TypedError{Code: easybus.ToolResultErrorCodeInvalidArgument, Message: "branch is required"}
	}
	gitArgs := []string{"checkout"}
	if argBool(args, "create", false) {
		gitArgs = append(gitArgs, "-b")
	}
	gitArgs = append(gitArgs, shellQuote(name))
	if _, err := r.Git(ctx, path, 60*time.Second, gitArgs...); err != nil {
		return extension.ToolResultData{}, err
	}
	return extension.ToolResultData{Content: i18n.T(t.locale(session, tenant), i18n.MsgCheckedOut, i18n.Params{"branch": name})}, nil
}

func (t *toolset) add(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	r, path, err := t.repo(ctx, args, session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	paths := argStrList(args, "paths")
	gitArgs := []string{"add"}
	if len(paths) == 0 {
		gitArgs = append(gitArgs, "-A")
	} else {
		for _, p := range paths {
			gitArgs = append(gitArgs, shellQuote(p))
		}
	}
	if _, err := r.Git(ctx, path, 60*time.Second, gitArgs...); err != nil {
		return extension.ToolResultData{}, err
	}
	return extension.ToolResultData{Content: i18n.T(t.locale(session, tenant), i18n.MsgStaged, i18n.Params{"n": fmt.Sprintf("%d", len(paths))})}, nil
}

func (t *toolset) commit(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	r, path, err := t.repo(ctx, args, session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	message := argStr(args, "message")
	if message == "" {
		return extension.ToolResultData{}, &extension.TypedError{Code: easybus.ToolResultErrorCodeInvalidArgument, Message: "message is required"}
	}
	gitArgs := []string{"commit", "-m", shellQuote(message)}
	if argBool(args, "all", false) {
		gitArgs = append(gitArgs, "-a")
	}
	// git refuses to commit without an identity, and a fresh sandbox has none:
	// the commit then fails with "Author identity unknown" and — worse — the tool
	// used to report success because it only checked the exit status of `git
	// rev-parse`, leaving the caller with an unchanged HEAD.
	//
	// `-c user.name=… -c user.email=…` sets it for this one command, so the
	// sandbox's own git config is untouched (an operator may have set it there,
	// and our value must not overwrite theirs for later manual use).
	author := argStr(args, "author")
	name, email := gitIdentity(author)
	gitArgs = append([]string{
		"-c", "user.name=" + name,
		"-c", "user.email=" + email,
	}, gitArgs...)
	if author != "" {
		gitArgs = append(gitArgs, "--author", shellQuote(author))
	}
	before, _ := r.Git(ctx, path, 30*time.Second, "rev-parse", "HEAD")
	if _, err := r.Git(ctx, path, 60*time.Second, gitArgs...); err != nil {
		return extension.ToolResultData{}, err
	}
	// Verify the commit actually happened. `git commit` can exit 0 while creating
	// nothing ("nothing to commit"), and the caller would otherwise be told a
	// commit was made and then find an unchanged HEAD — which is exactly what made
	// git-submit-mr refuse with "no commits that main does not already have".
	after, _ := r.Git(ctx, path, 30*time.Second, "rev-parse", "HEAD")
	if strings.TrimSpace(before) == strings.TrimSpace(after) {
		return extension.ToolResultData{}, &extension.TypedError{
			Code:    easybus.ToolResultErrorCodeInvalidArgument,
			Message: "nothing was committed: no staged changes. Run git-add (or pass all=true) before committing.",
		}
	}
	hash, _ := r.Git(ctx, path, 30*time.Second, "rev-parse", "--short", "HEAD")
	return extension.ToolResultData{Content: i18n.T(t.locale(session, tenant), i18n.MsgCommitted, i18n.Params{"hash": strings.TrimSpace(hash)}), Data: map[string]any{"hash": strings.TrimSpace(hash)}}, nil
}

// gitIdentity derives the committer name/email.
//
// An explicit `author` ("Name <email>") supplies both; otherwise the extension's
// own identity is used, so a commit always names who made it. This is what the
// review workflow needs: a merge request whose commits have no author is not
// reviewable.
func gitIdentity(author string) (string, string) {
	const defName, defEmail = "easyagent", "agent@easyagent.local"
	a := strings.TrimSpace(author)
	if a == "" {
		return defName, defEmail
	}
	// "Name <email>"
	if i := strings.LastIndex(a, "<"); i >= 0 {
		name := strings.TrimSpace(a[:i])
		mail := strings.TrimSuffix(strings.TrimSpace(a[i+1:]), ">")
		if name == "" {
			name = defName
		}
		if mail == "" {
			mail = defEmail
		}
		return name, mail
	}
	return a, defEmail
}

func (t *toolset) pull(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	r, path, err := t.repo(ctx, args, session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	auth := t.authFor(session, tenant, r.RemoteURL(ctx, path))
	pull := "git -C " + shellQuote(path) + " pull --ff-only"
	if auth.token != "" {
		pull = gitWithToken(pull, auth.user)
		if _, err := r.RunSecret(ctx, "", pull, auth.token, 300*time.Second); err != nil {
			return extension.ToolResultData{}, err
		}
	} else {
		if _, err := r.Run(ctx, "", pull, 300*time.Second); err != nil {
			return extension.ToolResultData{}, err
		}
	}
	br, _ := r.Git(ctx, path, 30*time.Second, "rev-parse", "--abbrev-ref", "HEAD")
	hash, _ := r.Git(ctx, path, 30*time.Second, "rev-parse", "--short", "HEAD")
	return extension.ToolResultData{Content: i18n.T(t.locale(session, tenant), i18n.MsgPulled, i18n.Params{"branch": strings.TrimSpace(br), "hash": strings.TrimSpace(hash)})}, nil
}

func (t *toolset) push(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	r, path, err := t.repo(ctx, args, session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	branch := argStr(args, "branch")
	push := "git -C " + shellQuote(path) + " push"
	if argBool(args, "force", false) {
		push += " --force-with-lease"
	}
	if branch != "" {
		push += " origin " + shellQuote(branch)
	}
	auth := t.authFor(session, tenant, r.RemoteURL(ctx, path))
	if auth.token != "" {
		push = gitWithToken(push, auth.user)
		if _, err := r.RunSecret(ctx, "", push, auth.token, 300*time.Second); err != nil {
			return extension.ToolResultData{}, err
		}
	} else {
		if _, err := r.Run(ctx, "", push, 300*time.Second); err != nil {
			return extension.ToolResultData{}, err
		}
	}
	if branch == "" {
		branch = "current branch"
	}
	return extension.ToolResultData{Content: i18n.T(t.locale(session, tenant), i18n.MsgPushed, i18n.Params{"branch": branch})}, nil
}

func (t *toolset) show(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	r, path, err := t.repo(ctx, args, session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	filePath := argStr(args, "path")
	rev := argStr(args, "rev")
	if rev == "" {
		rev = "HEAD"
	}
	if filePath == "" {
		return extension.ToolResultData{}, &extension.TypedError{Code: easybus.ToolResultErrorCodeInvalidArgument, Message: "path is required"}
	}
	out, err := r.Git(ctx, path, 60*time.Second, "show", shellQuote(rev+":"+filePath))
	if err != nil {
		return extension.ToolResultData{}, err
	}
	return extension.ToolResultData{Content: out}, nil
}
