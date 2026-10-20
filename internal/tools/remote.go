package tools

import (
	"context"
	"fmt"
	"strings"

	easybus "github.com/easydev-org/easybus-sdk"
	"github.com/easydev-org/easybus-sdk/extension"

	"github.com/easydev-org/easyagent-gitea-ext/internal/config"
	"github.com/easydev-org/easyagent-gitea-ext/internal/forge"
	"github.com/easydev-org/easyagent-gitea-ext/internal/i18n"
)

// These two tools read a REMOTE repository over the forge's HTTP API, so they
// need no sandbox and no clone — unlike every other git-* tool here.
//
// Why that matters: answering "what does the README say" or "which branches
// exist" used to require a clone into a sandbox first, which for a large repo is
// minutes of transfer to read a few hundred bytes. These answer in one request.

// remoteClient builds a forge client for the `remote` argument.
//
// The token comes from the matching entry in `servers`; an unconfigured host is
// tried anonymously (public repositories). The API base is derived from the
// server's kind/host, so no extra configuration is needed for a normal install.
func (t *toolset) remoteClient(session, tenant, remote string) (*forge.Client, config.Config, error) {
	cfg := t.cfg(session, tenant)
	host := config.HostOf(remote)
	if host == "" {
		return nil, cfg, &extension.TypedError{
			Code:    easybus.ToolResultErrorCodeInvalidArgument,
			Message: fmt.Sprintf("cannot parse a host from remote %q", remote),
		}
	}
	srv, ok := cfg.ServerForHost(host)
	if !ok {
		// No entry for this host: only a public repository can be read, and the
		// forge will say so. Failing here instead would be wrong — an anonymous
		// read of a public repo is a legitimate, common case.
		srv = config.Server{Host: host, Kind: config.KindGitea}
	}
	base := srv.APIBase()
	if base == "" {
		return nil, cfg, &extension.TypedError{
			Code:    easybus.ToolResultErrorCodeInvalidArgument,
			Message: fmt.Sprintf("%s is not configured as a Gitea/Forgejo host (set `kind` in the `servers` config), or use git-clone", host),
		}
	}
	cl, err := forge.New(base, srv.Token, remote)
	if err != nil {
		return nil, cfg, &extension.TypedError{
			Code:    easybus.ToolResultErrorCodeInvalidArgument,
			Message: err.Error(),
		}
	}
	return cl, cfg, nil
}

// remoteLs lists a remote repository's branches and tags without cloning it.
func (t *toolset) remoteLs(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	remote := argStr(args, "remote")
	if remote == "" {
		return extension.ToolResultData{}, &extension.TypedError{
			Code: easybus.ToolResultErrorCodeInvalidArgument, Message: "remote is required",
		}
	}
	cl, _, err := t.remoteClient(session, tenant, remote)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	refs, err := cl.ListRefs(ctx)
	if err != nil {
		return extension.ToolResultData{}, &extension.TypedError{
			Code: easybus.ToolResultErrorCodeBusiness, Message: err.Error(),
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s/%s\n", cl.Owner, cl.Repo)
	if len(refs) == 0 {
		b.WriteString(i18n.T(t.locale(session, tenant), i18n.MsgRemoteNoRefs, nil))
	}
	rows := make([]map[string]any, 0, len(refs))
	for _, r := range refs {
		// `*` marks the branch a clone would check out, matching the convention
		// git itself uses for `git branch`.
		mark := " "
		if r.Name == "main" || r.Name == "master" {
			mark = "*"
		}
		fmt.Fprintf(&b, "  %s %-8s %-40s %s\n", mark, r.Kind, r.Name, shortSHA(r.Commit))
		rows = append(rows, map[string]any{"name": r.Name, "kind": r.Kind, "commit": r.Commit})
	}
	return extension.ToolResultData{
		Content: strings.TrimRight(b.String(), "\n"),
		Data:    map[string]any{"owner": cl.Owner, "repo": cl.Repo, "refs": rows},
	}, nil
}

// remoteShow prints one file from a remote repository, without cloning it.
func (t *toolset) remoteShow(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	remote := argStr(args, "remote")
	path := argStr(args, "path")
	ref := argStr(args, "ref")
	if remote == "" || path == "" {
		return extension.ToolResultData{}, &extension.TypedError{
			Code: easybus.ToolResultErrorCodeInvalidArgument, Message: "remote and path are required",
		}
	}
	cl, _, err := t.remoteClient(session, tenant, remote)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	f, err := cl.ReadFile(ctx, path, ref)
	if err != nil {
		return extension.ToolResultData{}, &extension.TypedError{
			Code: easybus.ToolResultErrorCodeBusiness, Message: err.Error(),
		}
	}
	if f.Size > maxRemoteFileBytes {
		return extension.ToolResultData{}, &extension.TypedError{
			Code: easybus.ToolResultErrorCodeInvalidArgument,
			Message: fmt.Sprintf("%s is %d bytes, over the %d-byte limit for inline display; clone the repository to read it",
				path, f.Size, maxRemoteFileBytes),
		}
	}
	header := fmt.Sprintf("%s/%s:%s (%d bytes)\n", cl.Owner, cl.Repo, f.Path, f.Size)
	if ref != "" {
		header = fmt.Sprintf("%s/%s:%s@%s (%d bytes)\n", cl.Owner, cl.Repo, f.Path, ref, f.Size)
	}
	return extension.ToolResultData{
		Content: header + f.Content,
		Data: map[string]any{
			"owner": cl.Owner, "repo": cl.Repo, "path": f.Path,
			"ref": ref, "size": f.Size, "content": f.Content,
		},
	}, nil
}

// maxRemoteFileBytes caps what is inlined into a tool result. A source file is
// well under this; anything larger belongs in a clone, and a huge response would
// crowd out the rest of the conversation.
const maxRemoteFileBytes = 256 << 10

func shortSHA(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}
