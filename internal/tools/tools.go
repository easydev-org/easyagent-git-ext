// Package tools implements the git-* tool handlers. Every handler resolves the
// calling tenant's config, selects the server by the remote's host, and runs
// the operation under the per-repo lock.
package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/easydev-org/easyagent-git-ext/internal/config"
	"github.com/easydev-org/easyagent-git-ext/internal/git"
	"github.com/easydev-org/easyagent-git-ext/internal/i18n"
	"github.com/easydev-org/easyagent-git-ext/internal/workspace"
	easybus "github.com/easydev-org/easybus-sdk"
	"github.com/easydev-org/easybus-sdk/extension"
)

// Deps is what the tools need from the extension host.
type Deps struct {
	// Home is the repo-cache root.
	Home string
	// GetConfig reads an effective config value for the calling scope.
	GetConfig func(name, session, tenant string) any
	// Locale resolves the session locale for result text.
	Locale func(session, tenant string) string
}

// Handler is one tool implementation.
type Handler func(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error)

// Registry builds the git-* handler map.
func Registry(d Deps) (map[string]Handler, error) {
	mgr, err := workspace.New(d.Home)
	if err != nil {
		return nil, err
	}
	t := &toolset{deps: d, ws: mgr}
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
	}, nil
}

type toolset struct {
	deps Deps
	ws   *workspace.Manager
}

// cfgOf parses the caller's config.
func (t *toolset) cfgOf(session, tenant string) (config.Config, error) {
	get := func(name string) string {
		v := t.deps.GetConfig(name, session, tenant)
		if s, ok := v.(string); ok {
			return s
		}
		return ""
	}
	return config.Parse(get)
}

func (t *toolset) locale(session, tenant string) string {
	if t.deps.Locale != nil {
		return t.deps.Locale(session, tenant)
	}
	return ""
}

// authFor resolves the credential for a remote URL: the server whose host
// matches. A configured host with a token yields Basic auth; an unconfigured
// host is anonymous (public repos) unless require-auth is set.
func (t *toolset) authFor(cfg config.Config, remote string) (*git.Auth, error) {
	host := config.HostOf(remote)
	srv, ok := cfg.ServerForHost(host)
	if !ok {
		if cfg.RequireAuth {
			return nil, &extension.TypedError{
				Code:    easybus.ToolResultErrorCodeInvalidArgument,
				Message: fmt.Sprintf("no configured server for host %q (add it to the `servers` config)", host),
			}
		}
		return nil, nil
	}
	if srv.Token == "" {
		return nil, nil // anonymous
	}
	return &git.Auth{Username: srv.AuthUser(), Password: srv.Token}, nil
}

// resolve opens the repo for (tenant, remote), cloning it if absent.
func (t *toolset) resolve(ctx context.Context, cfg config.Config, session, tenant, remote string, depth int, branch string) (*git.Repo, func(), error) {
	if remote == "" {
		return nil, nil, &extension.TypedError{Code: easybus.ToolResultErrorCodeInvalidArgument, Message: "remote (a git URL) is required"}
	}
	lock := t.ws.Lock(tenant, remote)
	lock.Lock()
	unlock := func() { lock.Unlock() }

	auth, err := t.authFor(cfg, remote)
	if err != nil {
		unlock()
		return nil, nil, err
	}
	dir, err := t.ws.Ensure(tenant, remote)
	if err != nil {
		unlock()
		return nil, nil, err
	}
	if !git.IsRepo(dir) {
		if err := git.Clone(ctx, remote, dir, depth, branch, auth); err != nil {
			unlock()
			return nil, nil, &extension.TypedError{Code: easybus.ToolResultErrorCodeRetryable, Message: fmt.Sprintf("clone %s: %v", remote, err)}
		}
	}
	repo, err := git.Open(dir)
	if err != nil {
		unlock()
		return nil, nil, err
	}
	return repo, unlock, nil
}

// ---- handlers --------------------------------------------------------------

func (t *toolset) clone(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	cfg, err := t.cfgOf(session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	remote := argStr(args, "remote")
	if remote == "" {
		return extension.ToolResultData{}, &extension.TypedError{Code: easybus.ToolResultErrorCodeInvalidArgument, Message: "remote is required"}
	}
	depth := argInt(args, "depth", 1)
	branch := argStr(args, "branch")
	repo, unlock, err := t.resolve(ctx, cfg, session, tenant, remote, depth, branch)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	defer unlock()
	br, hash, _ := git.Head(repo)
	return extension.ToolResultData{
		Content: i18n.T(t.locale(session, tenant), i18n.MsgCloned, i18n.Params{"remote": remote, "branch": br, "hash": short(hash)}),
		Data:    map[string]any{"remote": remote, "branch": br, "hash": hash, "dir": t.wsRel(tenant, remote)},
	}, nil
}

func (t *toolset) status(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	cfg, err := t.cfgOf(session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	remote := argStr(args, "remote")
	repo, unlock, err := t.resolve(ctx, cfg, session, tenant, remote, 1, "")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	defer unlock()
	br, hash, _ := git.Head(repo)
	entries, err := git.Status(repo)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		lines = append(lines, fmt.Sprintf("%s%s %s", e.Staging, e.Worktree, e.Path))
	}
	body := strings.Join(lines, "\n")
	if body == "" {
		body = i18n.T(t.locale(session, tenant), i18n.MsgClean, nil)
	}
	return extension.ToolResultData{
		Content: fmt.Sprintf("%s %s\n%s", i18n.T(t.locale(session, tenant), i18n.MsgOnBranch, i18n.Params{"branch": br}), short(hash), body),
		Data:    map[string]any{"branch": br, "hash": hash, "changes": entries},
	}, nil
}

func (t *toolset) log(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	cfg, err := t.cfgOf(session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	remote := argStr(args, "remote")
	repo, unlock, err := t.resolve(ctx, cfg, session, tenant, remote, 1, "")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	defer unlock()
	entries, err := git.Log(repo, argInt(args, "limit", 20))
	if err != nil {
		return extension.ToolResultData{}, err
	}
	lines := make([]string, 0, len(entries))
	for _, c := range entries {
		lines = append(lines, fmt.Sprintf("%s %s (%s, %s)", short(c.Hash), c.Subject, c.Author, c.When.Format("2006-01-02")))
	}
	return extension.ToolResultData{Content: strings.Join(lines, "\n"), Data: map[string]any{"commits": entries}}, nil
}

func (t *toolset) diff(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	cfg, err := t.cfgOf(session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	remote := argStr(args, "remote")
	repo, unlock, err := t.resolve(ctx, cfg, session, tenant, remote, 1, "")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	defer unlock()
	from := argStr(args, "from")
	to := argStr(args, "to")
	if from == "" {
		from = "HEAD"
	}
	if to == "" {
		to = "HEAD"
	}
	files, err := git.DiffCommits(repo, from, to)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	var b strings.Builder
	for _, f := range files {
		fmt.Fprintf(&b, "%s (+%d -%d)\n", f.Path, f.Additions, f.Deletions)
		if argBool(args, "patch", false) {
			b.WriteString(f.Patch)
			b.WriteString("\n")
		}
	}
	return extension.ToolResultData{Content: strings.TrimRight(b.String(), "\n"), Data: map[string]any{"files": files}}, nil
}

func (t *toolset) branch(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	cfg, err := t.cfgOf(session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	remote := argStr(args, "remote")
	repo, unlock, err := t.resolve(ctx, cfg, session, tenant, remote, 1, "")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	defer unlock()
	branches, err := git.Branches(repo, argBool(args, "remoteOnly", false))
	if err != nil {
		return extension.ToolResultData{}, err
	}
	lines := make([]string, 0, len(branches))
	for _, b := range branches {
		mark := " "
		if b.Current {
			mark = "*"
		}
		lines = append(lines, fmt.Sprintf("%s %s %s", mark, b.Name, short(b.Hash)))
	}
	return extension.ToolResultData{Content: strings.Join(lines, "\n"), Data: map[string]any{"branches": branches}}, nil
}

func (t *toolset) checkout(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	cfg, err := t.cfgOf(session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	remote := argStr(args, "remote")
	name := argStr(args, "branch")
	if name == "" {
		return extension.ToolResultData{}, &extension.TypedError{Code: easybus.ToolResultErrorCodeInvalidArgument, Message: "branch is required"}
	}
	repo, unlock, err := t.resolve(ctx, cfg, session, tenant, remote, 1, "")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	defer unlock()
	if err := git.Checkout(repo, name, argBool(args, "create", false)); err != nil {
		return extension.ToolResultData{}, &extension.TypedError{Code: easybus.ToolResultErrorCodeBusiness, Message: fmt.Sprintf("checkout %s: %v", name, err)}
	}
	return extension.ToolResultData{Content: i18n.T(t.locale(session, tenant), i18n.MsgCheckedOut, i18n.Params{"branch": name})}, nil
}

func (t *toolset) add(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	cfg, err := t.cfgOf(session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	remote := argStr(args, "remote")
	repo, unlock, err := t.resolve(ctx, cfg, session, tenant, remote, 1, "")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	defer unlock()
	paths := argStrList(args, "paths")
	if err := git.Add(repo, paths); err != nil {
		return extension.ToolResultData{}, err
	}
	return extension.ToolResultData{Content: i18n.T(t.locale(session, tenant), i18n.MsgStaged, i18n.Params{"n": fmt.Sprintf("%d", len(paths))})}, nil
}

func (t *toolset) commit(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	cfg, err := t.cfgOf(session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	remote := argStr(args, "remote")
	message := argStr(args, "message")
	if message == "" {
		return extension.ToolResultData{}, &extension.TypedError{Code: easybus.ToolResultErrorCodeInvalidArgument, Message: "message is required"}
	}
	repo, unlock, err := t.resolve(ctx, cfg, session, tenant, remote, 1, "")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	defer unlock()
	hash, err := git.Commit(repo, git.CommitOpts{
		Message: message,
		Name:    argStr(args, "author"),
		Email:   argStr(args, "email"),
		All:     argBool(args, "all", false),
	})
	if err != nil {
		return extension.ToolResultData{}, &extension.TypedError{Code: easybus.ToolResultErrorCodeBusiness, Message: fmt.Sprintf("commit: %v", err)}
	}
	return extension.ToolResultData{Content: i18n.T(t.locale(session, tenant), i18n.MsgCommitted, i18n.Params{"hash": short(hash)}), Data: map[string]any{"hash": hash}}, nil
}

func (t *toolset) pull(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	cfg, err := t.cfgOf(session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	remote := argStr(args, "remote")
	repo, unlock, err := t.resolve(ctx, cfg, session, tenant, remote, 0, "")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	defer unlock()
	auth, _ := t.authFor(cfg, remote)
	if err := git.Pull(ctx, repo, auth); err != nil {
		return extension.ToolResultData{}, &extension.TypedError{Code: easybus.ToolResultErrorCodeRetryable, Message: fmt.Sprintf("pull: %v", err)}
	}
	br, hash, _ := git.Head(repo)
	return extension.ToolResultData{Content: i18n.T(t.locale(session, tenant), i18n.MsgPulled, i18n.Params{"branch": br, "hash": short(hash)})}, nil
}

func (t *toolset) push(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	cfg, err := t.cfgOf(session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	remote := argStr(args, "remote")
	repo, unlock, err := t.resolve(ctx, cfg, session, tenant, remote, 0, "")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	defer unlock()
	branch := argStr(args, "branch")
	if branch == "" {
		branch, _, _ = git.Head(repo)
	}
	force := argBool(args, "force", false)
	auth, _ := t.authFor(cfg, remote)
	if err := git.Push(ctx, repo, branch, force, auth); err != nil {
		return extension.ToolResultData{}, &extension.TypedError{Code: easybus.ToolResultErrorCodeRetryable, Message: fmt.Sprintf("push: %v", err)}
	}
	return extension.ToolResultData{Content: i18n.T(t.locale(session, tenant), i18n.MsgPushed, i18n.Params{"branch": branch})}, nil
}

func (t *toolset) show(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	cfg, err := t.cfgOf(session, tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	remote := argStr(args, "remote")
	path := argStr(args, "path")
	if path == "" {
		return extension.ToolResultData{}, &extension.TypedError{Code: easybus.ToolResultErrorCodeInvalidArgument, Message: "path is required"}
	}
	repo, unlock, err := t.resolve(ctx, cfg, session, tenant, remote, 1, "")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	defer unlock()
	rev := argStr(args, "rev")
	if rev == "" {
		rev = "HEAD"
	}
	content, err := git.ShowFile(repo, rev, path)
	if err != nil {
		return extension.ToolResultData{}, &extension.TypedError{Code: easybus.ToolResultErrorCodeNotFound, Message: fmt.Sprintf("%s not found at %s", path, rev)}
	}
	return extension.ToolResultData{Content: content}, nil
}

// ---- helpers ---------------------------------------------------------------

func short(hash string) string {
	if len(hash) > 8 {
		return hash[:8]
	}
	return hash
}

// wsRel is the repo's location relative to Home (for the tool result).
func (t *toolset) wsRel(tenant, remote string) string {
	p, err := t.ws.Path(tenant, remote)
	if err != nil {
		return ""
	}
	return p
}
