package github

import (
	"context"
	"fmt"

	gogithub "github.com/google/go-github/v93/github"
)

// PullRequest is this extension's view of a PR.
type PullRequest struct {
	Number  int
	Title   string
	State   string
	HTMLURL string
	Head    string
	Base    string
	Merged  bool
	// Mergeable is TRI-state on GitHub (nil while it computes), so a pointer.
	Mergeable *bool
}

func pullFromG(r *gogithub.PullRequest) PullRequest {
	return PullRequest{
		Number: r.GetNumber(), Title: r.GetTitle(), State: r.GetState(),
		HTMLURL: r.GetHTMLURL(), Head: r.GetHead().GetRef(), Base: r.GetBase().GetRef(),
		Merged: r.GetMerged(), Mergeable: r.Mergeable,
	}
}

// CreatePR opens a pull request.
//
// The branch must already exist: this extension pushes it first (via gitbridge) and only then
// asks for the PR, so a failed push never leaves an empty pull request behind.
func (c *Client) CreatePR(ctx context.Context, base, head, title, body string) (PullRequest, error) {
	// Head and Base are plain strings here (v93), not pointers — they are required fields, so
	// the API models them as values.
	in := gogithub.CreatePullRequest{Title: gogithub.String(title), Head: head, Base: base}
	if body != "" {
		in.Body = gogithub.String(body)
	}
	pr, resp, err := c.gh.PullRequests.Create(ctx, c.Owner, c.Repo, in)
	c.note(resp)
	if err != nil {
		return PullRequest{}, wrap(err)
	}
	return pullFromG(pr), nil
}

// PR reads one pull request.
func (c *Client) PR(ctx context.Context, number int) (PullRequest, error) {
	pr, resp, err := c.gh.PullRequests.Get(ctx, c.Owner, c.Repo, number)
	c.note(resp)
	if err != nil {
		return PullRequest{}, wrap(err)
	}
	return pullFromG(pr), nil
}

// ListPRs lists pull requests in one state (open|closed|all), newest first.
//
// Paged: a busy repository has more than one page, and returning only the first would hide
// reviews a caller was asking about.
func (c *Client) ListPRs(ctx context.Context, state string) ([]PullRequest, error) {
	if state == "" {
		state = "open"
	}
	var out []PullRequest
	err := paginate(func(page int) (*gogithub.Response, error) {
		prs, resp, err := c.gh.PullRequests.List(ctx, c.Owner, c.Repo, &gogithub.PullRequestListOptions{
			State: state, ListOptions: gogithub.ListOptions{Page: page, PerPage: 100},
		})
		c.note(resp)
		if err != nil {
			return resp, err
		}
		for _, p := range prs {
			out = append(out, pullFromG(p))
		}
		return resp, nil
	})
	return out, err
}

// MergePR merges a pull request.
//
// `method` is merge | squash | rebase. GitHub answers 200 with merged=false for a REFUSED merge
// (a conflict, a required check that has not passed), so that case is turned into an error —
// reporting success there would be a lie.
func (c *Client) MergePR(ctx context.Context, number int, method, title, message string) (PullRequest, error) {
	if method == "" {
		method = "merge"
	}
	opts := &gogithub.PullRequestOptions{MergeMethod: method}
	if title != "" {
		opts.CommitTitle = title
	}
	res, resp, err := c.gh.PullRequests.Merge(ctx, c.Owner, c.Repo, number, message, opts)
	c.note(resp)
	if err != nil {
		return PullRequest{}, wrap(err)
	}
	if !res.GetMerged() {
		return PullRequest{}, fmt.Errorf("merge refused: %s", res.GetMessage())
	}
	return c.PR(ctx, number)
}

// ClosePR closes a pull request without merging.
func (c *Client) ClosePR(ctx context.Context, number int) (PullRequest, error) {
	pr, resp, err := c.gh.PullRequests.Edit(ctx, c.Owner, c.Repo, number, &gogithub.PullRequest{
		State: gogithub.String("closed"),
	})
	c.note(resp)
	if err != nil {
		return PullRequest{}, wrap(err)
	}
	return pullFromG(pr), nil
}
