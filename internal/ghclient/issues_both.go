package ghclient

import (
	"context"

	gogithub "github.com/google/go-github/v93/github"
)

// ListIssuesBoth lists issues AND pull requests.
//
// Separate from ListIssues because the two answer different questions. ListIssues answers "what
// work is open?" and drops pull requests, since a caller asking for issues does not want them.
// This answers "what is numbered here?" and keeps the pull requests, which is what a caller needs
// when the number it holds might be either.
func (c *Client) ListIssuesBoth(ctx context.Context, o ListIssuesOptions) ([]Issue, error) {
	state := o.State
	if state == "" {
		state = "open"
	}
	limit := o.Limit
	if limit <= 0 {
		limit = 30
	}
	var out []Issue
	err := paginate(func(page int) (*gogithub.Response, error) {
		items, resp, err := c.gh.Issues.ListByRepo(ctx, c.Owner, c.Repo,
			&gogithub.IssueListByRepoOptions{
				State: state, Labels: labelList(o.Label),
				ListOptions: gogithub.ListOptions{Page: page, PerPage: 100},
			})
		c.note(resp)
		if err != nil {
			return resp, err
		}
		for _, it := range items {
			if len(out) >= limit {
				break
			}
			out = append(out, issueFromG(it))
		}
		return resp, nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
