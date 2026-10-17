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

	"github.com/easydev-org/easyagent-git-ext/internal/config"
	"github.com/easydev-org/easyagent-git-ext/internal/i18n"
	"github.com/easydev-org/easyagent-git-ext/internal/sandbox"
	easybus "github.com/easydev-org/easybus-sdk"
	"github.com/easydev-org/easybus-sdk/extension"
)

// Deps is what the tools need from the extension host.
type Deps struct {
	GetConfig func(name, session, tenant string) any
	Locale    func(session, tenant string) string
	Cache     *sandbox.ClientCache
}

// Handler is one tool implementation.
type Handler func(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error)

// Registry builds the git-* handler map.
func Registry(d Deps) map[string]Handler {
	t := &toolset{deps: d}
	return map[string]Handler{
		"git-clone":    t.clone,
		"git-status":   t.status,
		"git-log":      t.log,
		"git-diff":     t.diff,
		"git-branch":   t.branch,
		"git-checkout": t.checkout,
		"git-add":      t.add,
		"git-commit":   t.commit,
		"git-pull":     t.pull,
		"git-push":     t.push,
		"git-show":     t.show,
	}
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
	if a := argStr(args, "author"); a != "" {
		gitArgs = append(gitArgs, "--author", shellQuote(a))
	}
	if _, err := r.Git(ctx, path, 60*time.Second, gitArgs...); err != nil {
		return extension.ToolResultData{}, err
	}
	hash, _ := r.Git(ctx, path, 30*time.Second, "rev-parse", "--short", "HEAD")
	return extension.ToolResultData{Content: i18n.T(t.locale(session, tenant), i18n.MsgCommitted, i18n.Params{"hash": strings.TrimSpace(hash)}), Data: map[string]any{"hash": strings.TrimSpace(hash)}}, nil
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
