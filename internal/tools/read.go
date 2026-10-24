package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/easydev-org/easybus-sdk/extension"

	"github.com/easydev-org/easyagent-github-ext/internal/ghclient"
)

// Reading a public repository. None of these require a token — that is the whole point of the
// extension, and the handler does not even look for one.

// clientFor builds a client for the repository named in the arguments, with the tenant's token
// when one is configured (reads work without it; writes do not).
func (t *toolset) clientFor(tenant string, args map[string]any) (*ghclient.Client, string, error) {
	repo, err := required(args, "repo")
	if err != nil {
		return nil, "", err
	}
	remote, err := remoteFor(repo)
	if err != nil {
		return nil, "", err
	}
	c, err := ghclient.New(ghclient.Options{Remote: remote, Token: t.token(tenant)})
	if err != nil {
		return nil, "", errArg(err.Error())
	}
	return c, repo, nil
}

// remoteFor turns "owner/name" or a full URL into a remote URL.
func remoteFor(repo string) (string, error) {
	if strings.Contains(repo, "://") || strings.HasPrefix(repo, "git@") {
		return repo, nil
	}
	parts := strings.Split(strings.Trim(repo, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", errArg(fmt.Sprintf("repo must be owner/name (got %q)", repo))
	}
	return "https://github.com/" + parts[0] + "/" + parts[1], nil
}

// token reads the tenant's GitHub token from configuration.
//
// One token per tenant, not per repository: a session works within one identity, and letting a
// tool call name a different credential would be a way to act as somebody else.
func (t *toolset) token(tenant string) string {
	if t.deps.GetConfig == nil {
		return ""
	}
	if s, ok := t.deps.GetConfig("token", "", tenant).(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func (t *toolset) info(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	c, repo, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	r, err := c.Info(ctx)
	if err != nil {
		return extension.ToolResultData{}, errFailed("%s: %v", repo, err)
	}
	lines := []string{
		fmt.Sprintf("%s", r.FullName),
		fmt.Sprintf("default branch: %s", r.DefaultBranch),
		fmt.Sprintf("private: %v", r.Private),
	}
	if r.Description != "" {
		lines = append(lines, "description: "+r.Description)
	}
	if r.Language != "" {
		lines = append(lines, "language: "+r.Language)
	}
	if r.License != "" {
		lines = append(lines, "license: "+r.License)
	}
	lines = append(lines, fmt.Sprintf("stars: %d", r.Stars))
	if r.Archived {
		lines = append(lines, "ARCHIVED")
	}
	return extension.ToolResultData{
		Content: strings.Join(lines, "\n"),
		Data:    map[string]any{"repo": r},
	}, nil
}

// numberLines prefixes each line with its 1-based number.
func numberLines(s string) string {
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	var b strings.Builder
	w := len(fmt.Sprintf("%d", len(lines)))
	for i, l := range lines {
		fmt.Fprintf(&b, "%*d| %s\n", w, i+1, l)
	}
	return b.String()
}
