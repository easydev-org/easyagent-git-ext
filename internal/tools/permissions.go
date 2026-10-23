package tools

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/easydev-org/easyagent-gitea-ext/internal/config"
	easybus "github.com/easydev-org/easybus-sdk"
	"github.com/easydev-org/easybus-sdk/extension"
)

// Permission enforcement, gitea side.
//
// The agent hands each call the session's grants and checks that a capability of the right
// KIND exists. Whether a particular grant COVERS this call is decided here, because only
// this extension knows that a git remote URL denotes a repository and that "agentdev/*"
// should include "agentdev/mrflow".
//
// This is the real boundary: the forge token lives in this process, so a call that gets
// past this file is the one that will actually merge. The agent's check is an earlier,
// coarser one with a friendlier message.

// Permission keys this extension declares. Exported so the manifest and the tools cannot
// drift: a tool names one of these, and the manifest documents the same string.
const (
	// PermMRMerge allows merging a pull request.
	PermMRMerge = "mr.merge"
	// PermMRClose allows closing without merging.
	PermMRClose = "mr.close"
	// PermRepoCreate allows creating a repository.
	PermRepoCreate = "repo.create"
	// PermRepoDelete allows deleting a repository.
	PermRepoDelete = "repo.delete"
)

// repoSlug reduces a remote URL to "owner/name", which is what a grant's `repo` param
// names.
//
// Handles the three shapes a git remote takes: https://host/o/r(.git), scp-style
// git@host:o/r(.git), and a bare path. Returns "" when the URL has fewer than two path
// segments — a grant can then never cover it, which is the safe answer for a remote we
// cannot identify.
func repoSlug(remote string) string {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return ""
	}
	var path string
	if strings.Contains(remote, "://") {
		u, err := url.Parse(remote)
		if err != nil {
			return ""
		}
		path = u.Path
	} else if at := strings.IndexByte(remote, '@'); at >= 0 {
		// scp-style: git@host:owner/repo.git
		rest := remote[at+1:]
		if colon := strings.IndexByte(rest, ':'); colon >= 0 {
			path = rest[colon+1:]
		} else {
			return ""
		}
	} else {
		path = remote
	}

	path = strings.Trim(path, "/")
	path = strings.TrimSuffix(path, ".git")
	segs := strings.Split(path, "/")
	if len(segs) < 2 {
		return ""
	}
	owner, name := segs[len(segs)-2], segs[len(segs)-1]
	if owner == "" || name == "" {
		return ""
	}
	return owner + "/" + name
}

// requireRepo resolves the target repository of a call and checks a permission against it.
//
// The target is derived from the call's own `remote` argument (or the selected sandbox's
// configured remote when the argument is absent), so a caller cannot name one repository in
// the arguments and be authorised for another.
func (t *toolset) requireRepo(ctx context.Context, perm string, args map[string]any, session, tenant string) error {
	remote := argStr(args, "remote")
	if remote == "" {
		// No explicit remote: the call targets the selected sandbox's repository, so THAT
		// is the target. A failure to resolve it is reported as a permission error rather
		// than a separate failure mode — either way the call cannot be authorised.
		r, path, err := t.repo(ctx, args, session, tenant)
		if err != nil {
			return err
		}
		remote = r.RemoteURL(ctx, path)
	}
	slug := repoSlug(remote)
	// An unparseable remote resolves to "", and a wildcard grant WOULD match "" — so the
	// check must fail before it, not rely on matching to do the right thing. This is the
	// fail-closed case: if we cannot name the repository, we cannot authorise a write to it.
	if slug == "" {
		return &extension.TypedError{
			Code: easybus.ToolResultErrorCodeInvalidArgument,
			Message: fmt.Sprintf(
				"cannot identify a repository from remote %q, so %s cannot be authorised; "+
					"pass a remote like https://host/owner/name.git", remote, perm),
		}
	}
	return extension.Require(ctx, perm, map[string]string{"repo": slug})
}

// ensurePermKeysDeclared asserts every key a tool requires is declared in the manifest.
//
// Called from a test: a tool gated on an undeclared key would compile, register, and then
// refuse every call with no explanation of what to grant.
func EnsurePermKeysDeclared(declared map[string]bool, keys []string) []string {
	var missing []string
	for _, k := range keys {
		if !declared[k] {
			missing = append(missing, k)
		}
	}
	return missing
}

var _ = config.HostOf
