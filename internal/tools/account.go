package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/easydev-org/easybus-sdk/extension"

	"github.com/easydev-org/easyagent-git-ext/internal/ghclient"
)

// Account tools: who the token is, what it may do, and what it can see.
//
// These need a token. There is no useful anonymous answer to "which organizations do I belong
// to", so the handler says so instead of returning an empty list.

// accountClient builds a token-bearing client that is not tied to one repository.
func (t *toolset) accountClient(tenant string) (*ghclient.Client, error) {
	tok := t.token(tenant)
	if tok == "" {
		return nil, errArg("this needs a GitHub token; set the `token` setting")
	}
	return ghclient.New(ghclient.Options{Token: tok})
}

func (t *toolset) whoami(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	Progress(ctx).Phase("identifying the token")
	c, err := t.accountClient(tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	id, sc, err := c.IdentityAndScopes(ctx)
	if err != nil {
		return extension.ToolResultData{}, errFailed("identify: %v", err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "login: %s\n", id.Login)
	if id.Name != "" {
		fmt.Fprintf(&b, "name: %s\n", id.Name)
	}
	fmt.Fprintf(&b, "type: %s\n", id.Type)
	if sc.FineGrained {
		// Saying "no scopes" here would be wrong: a fine-grained token has per-repository
		// permissions that GitHub does not report as scopes at all.
		b.WriteString("token: fine-grained (per-repository permissions; no scope list)\n")
	} else {
		fmt.Fprintf(&b, "scopes: %s\n", strings.Join(sc.Classic, ", "))
	}
	if err := ghclient.CheckTokenCanDelete(sc); err != nil {
		fmt.Fprintf(&b, "delete repos: NOT permitted (%v)\n", err)
	} else {
		b.WriteString("delete repos: permitted\n")
	}
	if sc.ExpiresAt != "" {
		fmt.Fprintf(&b, "expires: %s\n", sc.ExpiresAt)
	}
	if rem, reset, ok := c.RateBudget(); ok {
		fmt.Fprintf(&b, "rate limit: %d left, resets %s\n", rem, reset.Format("15:04:05"))
	}
	return extension.ToolResultData{
		Content: b.String(),
		Data:    map[string]any{"identity": id, "scopes": sc},
	}, nil
}

func (t *toolset) orgs(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	Progress(ctx).Phase("listing organizations")
	c, err := t.accountClient(tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	list, err := c.Orgs(ctx)
	if err != nil {
		return extension.ToolResultData{}, errFailed("orgs: %v", err)
	}
	var b strings.Builder
	for _, o := range list {
		if o.Description != "" {
			fmt.Fprintf(&b, "%s — %s\n", o.Login, o.Description)
		} else {
			fmt.Fprintf(&b, "%s\n", o.Login)
		}
	}
	if len(list) == 0 {
		b.WriteString("(no organizations)\n")
	}
	return extension.ToolResultData{
		Content: b.String(),
		Data:    map[string]any{"orgs": list, "count": len(list)},
	}, nil
}

// repos lists repositories: one organization's when `org` is given, otherwise everything the
// caller can see.
func (t *toolset) repos(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	Progress(ctx).Phase("listing repositories")
	c, err := t.accountClient(tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	var list []ghclient.RepoRef
	if org := argStr(args, "org"); org != "" {
		list, err = c.ListOrgRepos(ctx, org, argStr(args, "type"))
	} else {
		list, err = c.ListUserRepos(ctx, argStr(args, "visibility"))
	}
	if err != nil {
		return extension.ToolResultData{}, errFailed("repos: %v", err)
	}
	var b strings.Builder
	for _, r := range list {
		mark := ""
		if r.Private {
			mark = " [private]"
		}
		if r.Archived {
			mark += " [archived]"
		}
		fmt.Fprintf(&b, "%s%s\n", r.FullName, mark)
	}
	fmt.Fprintf(&b, "\n%d repositories", len(list))
	return extension.ToolResultData{
		Content: b.String(),
		Data:    map[string]any{"repos": list, "count": len(list)},
	}, nil
}

func (t *toolset) releases(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	Progress(ctx).Phase("listing releases")
	c, repo, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	tag := argStr(args, "tag")
	if tag != "" {
		r, err := c.Release(ctx, tag)
		if err != nil {
			return extension.ToolResultData{}, errFailed("%s@%s: %v", repo, tag, err)
		}
		return extension.ToolResultData{
			Content: fmt.Sprintf("%s\n%s\n%s\n\n%s", r.TagName, r.Name, r.HTMLURL, r.Body),
			Data:    map[string]any{"release": r},
		}, nil
	}

	list, err := c.Releases(ctx, argInt(args, "limit"))
	if err != nil {
		return extension.ToolResultData{}, errFailed("%s: %v", repo, err)
	}
	var b strings.Builder
	for _, r := range list {
		mark := ""
		if r.Draft {
			mark = " [draft]"
		}
		if r.Prerelease {
			mark += " [pre]"
		}
		fmt.Fprintf(&b, "%s%s — %s\n", r.TagName, mark, r.PublishedAt)
	}
	if len(list) == 0 {
		b.WriteString("(no releases)\n")
	}
	fmt.Fprintf(&b, "\n%d releases", len(list))
	return extension.ToolResultData{
		Content: b.String(),
		Data:    map[string]any{"releases": list, "count": len(list)},
	}, nil
}

func (t *toolset) packages(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	Progress(ctx).Phase("listing packages")
	c, err := t.accountClient(tenant)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	org, err := required(args, "org")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	kind := argStr(args, "kind")

	// One package's versions, when a name is given.
	if name := argStr(args, "package"); name != "" {
		vers, verr := c.PackageVersions(ctx, org, kind, name)
		if verr != nil {
			return extension.ToolResultData{}, errFailed("%s/%s: %v", org, name, verr)
		}
		var b strings.Builder
		for _, v := range vers {
			fmt.Fprintf(&b, "%s\n", v)
		}
		fmt.Fprintf(&b, "\n%d versions", len(vers))
		return extension.ToolResultData{
			Content: b.String(),
			Data:    map[string]any{"versions": vers, "count": len(vers)},
		}, nil
	}

	list, err := c.ListOrgPackages(ctx, org, kind)
	if err != nil {
		return extension.ToolResultData{}, errFailed("%s packages: %v", org, err)
	}
	var b strings.Builder
	for _, p := range list {
		fmt.Fprintf(&b, "%s (%s, %s)\n", p.Name, p.Type, p.Visibility)
	}
	if len(list) == 0 {
		b.WriteString("(no packages)\n")
	}
	fmt.Fprintf(&b, "\n%d packages", len(list))
	return extension.ToolResultData{
		Content: b.String(),
		Data:    map[string]any{"packages": list, "count": len(list)},
	}, nil
}
