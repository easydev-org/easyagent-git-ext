// Package forge reads a REMOTE Gitea/Forgejo repository over its HTTP API,
// without cloning it.
//
// Why an API and not a clone: reading one file should not download the history
// (or even one full snapshot). A clone of a large repo to answer "what does
// README say" costs minutes of transfer; the API answers it in one request.
//
// Partial clone (`--filter=blob:none`) was the other candidate and is rejected
// on purpose: it still writes a repository to disk, still needs a working tree,
// and its cost is paid on every call. The API is stateless.
//
// SCOPE: this is a thin wrapper over the OFFICIAL SDK (code.gitea.io/sdk/gitea),
// which already handles Gitea's API shapes, pagination parameters and error
// mapping. Writing that by hand was the first attempt and it got the pagination
// parameter wrong (`limit` here, `per_page` elsewhere) — exactly the class of
// drift an upstream SDK absorbs for us.
//
// Forgejo advertises Gitea API compatibility, so the same client serves both.
package forge

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"code.gitea.io/sdk/gitea"
)

// ErrUnsupported is returned when the configured host is not a forge this
// package can talk to.
var ErrUnsupported = errors.New("this tool reads repositories over the Gitea/Forgejo API, so the host must be configured with kind=gitea (or use git-clone)")

// Ref is one branch or tag of a remote repository.
type Ref struct {
	Name   string
	Commit string
	// Kind is "branch" or "tag".
	Kind string
}

// File is one remote file's content plus the revision it came from.
type File struct {
	Path     string
	Revision string
	Content  string
	Size     int
}

// Client reads one remote repository.
type Client struct {
	c *gitea.Client
	// Owner and Repo are the path segments of the repository.
	Owner string
	Repo  string
}

// New builds a Client from a repository URL and the host's settings.
//
// `baseURL` is the Gitea origin (scheme + host, optionally a sub-path); the SDK
// appends /api/v1 itself. An empty token reads public repositories anonymously.
func New(baseURL, token, remoteURL string) (*Client, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, ErrUnsupported
	}
	owner, repo, err := splitRepoPath(remoteURL)
	if err != nil {
		return nil, err
	}
	opts := []gitea.ClientOption{}
	if t := strings.TrimSpace(token); t != "" {
		// SetToken is a CONSTRUCTOR option, not a method: the credential must be
		// known when the client is built.
		opts = append(opts, gitea.SetToken(t))
	}
	c, err := gitea.NewClient(strings.TrimRight(baseURL, "/"), opts...)
	if err != nil {
		return nil, fmt.Errorf("gitea client for %q: %w", baseURL, err)
	}
	return &Client{c: c, Owner: owner, Repo: repo}, nil
}

// splitRepoPath extracts (owner, repo) from a remote URL.
//
// It accepts the shapes a remote actually takes: https://host/owner/repo(.git),
// git@host:owner/repo.git, and a trailing slash. Gitea allows nested owners
// (`org/sub/repo`), so the repo is the LAST segment.
func splitRepoPath(remote string) (string, string, error) {
	s := strings.TrimSpace(remote)
	if s == "" {
		return "", "", fmt.Errorf("remote is required")
	}
	// A URL with a scheme is parsed as such. This check must come FIRST: an
	// `https://host/...` also contains a ':' before any '/', so an scp-style test
	// written the other way round silently treats "https" as a hostname and puts
	// the real host into the owner segment.
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil || u.Host == "" {
			return "", "", fmt.Errorf("cannot parse %q as a URL", remote)
		}
		// The PATH is what carries owner/repo; without this the host would end up
		// in the owner and every API call would 404.
		s = u.Path
	} else if i := strings.Index(s, ":"); i > 0 {
		// scp-like syntax: [user@]host:owner/repo.git
		s = s[i+1:]
	}
	s = strings.Trim(s, "/")
	s = strings.TrimSuffix(s, ".git")
	parts := strings.Split(s, "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("cannot derive owner/repo from %q", remote)
	}
	return strings.Join(parts[:len(parts)-1], "/"), parts[len(parts)-1], nil
}

// ListRefs returns the branches and tags, branches first then tags, each group
// sorted by name.
//
// Pagination: the SDK takes an explicit page/page-size and does NOT iterate for
// us, so this loop must advance the page or a repository with more refs than one
// page would silently under-report.
func (c *Client) ListRefs(ctx context.Context) ([]Ref, error) {
	out := []Ref{}

	for page := 1; page <= 200; page++ {
		branches, _, err := c.c.ListRepoBranches(c.Owner, c.Repo, gitea.ListRepoBranchesOptions{
			ListOptions: gitea.ListOptions{Page: page, PageSize: listPageSize},
		})
		if err != nil {
			return nil, mapErr(err)
		}
		for _, b := range branches {
			out = append(out, Ref{Name: b.Name, Commit: b.Commit.ID, Kind: "branch"})
		}
		if len(branches) < listPageSize {
			break
		}
	}

	for page := 1; page <= 200; page++ {
		tags, _, err := c.c.ListRepoTags(c.Owner, c.Repo, gitea.ListRepoTagsOptions{
			ListOptions: gitea.ListOptions{Page: page, PageSize: listPageSize},
		})
		if err != nil {
			return nil, mapErr(err)
		}
		for _, t := range tags {
			// A Tag carries its own ID; CommitMeta has SHA (not ID). Prefer the
			// commit meta, fall back to the tag's own id (an annotated tag points
			// at a tag object, and its Commit is what a reader wants).
			sha := ""
			if t.Commit != nil {
				sha = t.Commit.SHA
			}
			if sha == "" {
				sha = t.ID
			}
			out = append(out, Ref{Name: t.Name, Commit: sha, Kind: "tag"})
		}
		if len(tags) < listPageSize {
			break
		}
	}

	sortRefs(out)
	return out, nil
}

// listPageSize is the API page size; 50 is Gitea's own default maximum for most
// endpoints and keeps the request count low without assuming a bigger cap.
const listPageSize = 50

// ReadFile returns one file's content at ref. An empty ref means the repository's
// default branch.
func (c *Client) ReadFile(ctx context.Context, path, ref string) (File, error) {
	path = strings.Trim(path, "/")
	if path == "" {
		return File{}, fmt.Errorf("path is required")
	}
	content, _, err := c.c.GetFile(c.Owner, c.Repo, ref, path)
	if err != nil {
		return File{}, mapErr(err)
	}
	return File{Path: path, Revision: ref, Content: string(content), Size: len(content)}, nil
}

// mapErr turns the SDK's error into something a caller can act on.
//
// The SDK reports a missing path/repo as a plain message ("The target couldn't be
// found.") without saying WHICH of the two common causes applies, so the hint is
// added here rather than repeated at every call site.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "token is required"):
		return fmt.Errorf("this repository needs authentication: add a token for its host in the `servers` config")
	case strings.Contains(msg, "couldn't be found"), strings.Contains(msg, "not found"):
		return fmt.Errorf("%s (check the path/ref, or that the token can read this repository)", msg)
	default:
		return err
	}
}

// PullRequest is one merge/pull request, in the shape the tools report.
type PullRequest struct {
	Index int64
	Title string
	State string
	Head  string
	Base  string
	URL   string
	// Mergeable reports whether the forge believes the change applies cleanly.
	Mergeable bool
}

// OpenPullRequest creates a merge request from head into base.
//
// The head branch must already exist on the remote — this call only opens the
// request, it does not upload commits. The caller pushes first (inside the
// sandbox), so the forge never sees a request for a branch it does not have.
func (c *Client) OpenPullRequest(ctx context.Context, base, head, title, body string) (PullRequest, error) {
	pr, _, err := c.c.CreatePullRequest(c.Owner, c.Repo, gitea.CreatePullRequestOption{
		Head: head, Base: base, Title: title, Body: body,
	})
	if err != nil {
		return PullRequest{}, mapErr(err)
	}
	return toPullRequest(pr), nil
}

// ListPullRequests lists merge requests in the given state
// ("open" | "closed" | "all").
func (c *Client) ListPullRequests(ctx context.Context, state string) ([]PullRequest, error) {
	var out []PullRequest
	for page := 1; page <= 200; page++ {
		prs, _, err := c.c.ListRepoPullRequests(c.Owner, c.Repo, gitea.ListPullRequestsOptions{
			State:       gitea.StateType(state),
			ListOptions: gitea.ListOptions{Page: page, PageSize: listPageSize},
		})
		if err != nil {
			return nil, mapErr(err)
		}
		for _, pr := range prs {
			out = append(out, toPullRequest(pr))
		}
		if len(prs) < listPageSize {
			break
		}
	}
	return out, nil
}

func toPullRequest(pr *gitea.PullRequest) PullRequest {
	if pr == nil {
		return PullRequest{}
	}
	out := PullRequest{
		// The SDK calls the PR number `Index` (its JSON key is "number").
		Index: pr.Index, Title: pr.Title, URL: pr.HTMLURL,
		State: string(pr.State),
	}
	if pr.Head != nil {
		out.Head = pr.Head.Ref
	}
	if pr.Base != nil {
		out.Base = pr.Base.Ref
	}
	return out
}

// sortRefs orders branches before tags, then by name, so the output is stable.
func sortRefs(refs []Ref) {
	// A tiny insertion sort keeps this dependency-free and the slices are short.
	for i := 1; i < len(refs); i++ {
		for j := i; j > 0 && less(refs[j], refs[j-1]); j-- {
			refs[j], refs[j-1] = refs[j-1], refs[j]
		}
	}
}

func less(a, b Ref) bool {
	if a.Kind != b.Kind {
		return a.Kind == "branch"
	}
	return a.Name < b.Name
}
