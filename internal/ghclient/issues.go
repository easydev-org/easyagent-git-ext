package ghclient

import (
	"context"
	"fmt"

	gogithub "github.com/google/go-github/v93/github"
)

// Issues, and the comments that go on them and on pull requests.
//
// WHY THIS IS NOT UNDER "read": an issue is how work gets REQUESTED, and a comment is how a
// reviewer says something. Without them an agent can see a pull request but not answer it — it
// could only close or merge, which is the opposite of review.

// Issue is one issue, flattened to what a caller needs.
type Issue struct {
	Number    int
	Title     string
	State     string
	Body      string
	Author    string
	Labels    []string
	Comments  int
	HTMLURL   string
	CreatedAt string
	UpdatedAt string
	// IsPullRequest is true for the entries that GitHub returns in the issue list but which are
	// actually pull requests. They share one numbering space, so a caller reading "issues" can be
	// handed a pull request unless this is checked.
	IsPullRequest bool
}

// ListIssuesOptions filters the issue list.
type ListIssuesOptions struct {
	State string // open | closed | all
	Label string
	Limit int
}

// ListIssues lists issues. Pull requests are filtered OUT unless IncludePullRequests is set:
// GitHub returns both from the issues endpoint, and a caller asking for issues almost never wants
// the pull requests mixed in.
func (c *Client) ListIssues(ctx context.Context, o ListIssuesOptions) ([]Issue, error) {
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
			// The issues endpoint includes pull requests. Skipped, not shown and labelled:
			// a caller listing issues wants issues.
			if it.IsPullRequest() {
				continue
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

func labelList(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

func issueFromG(i *gogithub.Issue) Issue {
	out := Issue{
		Number:        i.GetNumber(),
		Title:         i.GetTitle(),
		State:         i.GetState(),
		Body:          i.GetBody(),
		Comments:      i.GetComments(),
		HTMLURL:       i.GetHTMLURL(),
		IsPullRequest: i.IsPullRequest(),
	}
	if u := i.GetUser(); u != nil {
		out.Author = u.GetLogin()
	}
	for _, l := range i.Labels {
		out.Labels = append(out.Labels, l.GetName())
	}
	if p := i.CreatedAt.GetTime(); p != nil {
		out.CreatedAt = p.Format("2006-01-02 15:04")
	}
	if p := i.UpdatedAt.GetTime(); p != nil {
		out.UpdatedAt = p.Format("2006-01-02 15:04")
	}
	return out
}

// Issue returns one issue.
func (c *Client) Issue(ctx context.Context, number int) (Issue, error) {
	i, resp, err := c.gh.Issues.Get(ctx, c.Owner, c.Repo, number)
	c.note(resp)
	if err != nil {
		return Issue{}, wrap(err)
	}
	return issueFromG(i), nil
}

// CreateIssueOptions describes a new issue.
type CreateIssueOptions struct {
	Title  string
	Body   string
	Labels []string
}

// CreateIssue opens an issue and returns it.
func (c *Client) CreateIssue(ctx context.Context, o CreateIssueOptions) (Issue, error) {
	if o.Title == "" {
		return Issue{}, fmt.Errorf("a title is required: an issue without one cannot be found or discussed")
	}
	req := gogithub.CreateIssueRequest{Title: o.Title, Labels: o.Labels}
	if o.Body != "" {
		req.Body = gogithub.Ptr(o.Body)
	}
	i, resp, err := c.gh.Issues.Create(ctx, c.Owner, c.Repo, req)
	c.note(resp)
	if err != nil {
		return Issue{}, wrap(err)
	}
	return issueFromG(i), nil
}

// CloseIssue closes an issue.
func (c *Client) CloseIssue(ctx context.Context, number int) error {
	state := "closed"
	_, resp, err := c.gh.Issues.Update(ctx, c.Owner, c.Repo, number,
		gogithub.UpdateIssueRequest{State: &state})
	c.note(resp)
	return wrap(err)
}

// Comment is one comment on an issue or pull request.
type Comment struct {
	ID     int64
	Body   string
	Author string
	When   string
	URL    string
	// Path and Line are set for a review comment that points at code. Empty for a conversation
	// comment, which is the difference a reader needs to tell "about line 42" from "about the PR".
	Path string
	Line int
	// ReplyTo is set when this comment answers another one.
	ReplyTo int64
}

// IssueComments lists the comments on an issue OR a pull request.
//
// One call for both because GitHub treats them as the same resource: a pull request's
// conversation comments live on the issues endpoint, which is a consequence of the two sharing a
// numbering space.
func (c *Client) IssueComments(ctx context.Context, number int) ([]Comment, error) {
	var out []Comment
	err := paginate(func(page int) (*gogithub.Response, error) {
		items, resp, err := c.gh.Issues.ListComments(ctx, c.Owner, c.Repo, number,
			&gogithub.IssueListCommentsOptions{
				ListOptions: gogithub.ListOptions{Page: page, PerPage: 100},
			})
		c.note(resp)
		if err != nil {
			return resp, err
		}
		for _, it := range items {
			out = append(out, commentFromIssue(it))
		}
		return resp, nil
	})
	return out, err
}

func commentFromIssue(i *gogithub.IssueComment) Comment {
	out := Comment{ID: i.GetID(), Body: i.GetBody(), URL: i.GetHTMLURL()}
	if u := i.GetUser(); u != nil {
		out.Author = u.GetLogin()
	}
	if p := i.CreatedAt.GetTime(); p != nil {
		out.When = p.Format("2006-01-02 15:04")
	}
	return out
}

// CreateIssueComment adds a conversation comment to an issue or pull request.
func (c *Client) CreateIssueComment(ctx context.Context, number int, body string) (Comment, error) {
	if body == "" {
		return Comment{}, fmt.Errorf("a comment body is required: an empty comment notifies every " +
			"watcher and says nothing")
	}
	it, resp, err := c.gh.Issues.CreateComment(ctx, c.Owner, c.Repo, number,
		gogithub.IssueCommentRequest{Body: body})
	c.note(resp)
	if err != nil {
		return Comment{}, wrap(err)
	}
	return commentFromIssue(it), nil
}

// PRComments lists a pull request's review comments — the ones attached to a file or a line.
func (c *Client) PRComments(ctx context.Context, number int) ([]Comment, error) {
	var out []Comment
	err := paginate(func(page int) (*gogithub.Response, error) {
		items, resp, err := c.gh.PullRequests.ListComments(ctx, c.Owner, c.Repo, number,
			&gogithub.PullRequestListCommentsOptions{
				ListOptions: gogithub.ListOptions{Page: page, PerPage: 100},
			})
		c.note(resp)
		if err != nil {
			return resp, err
		}
		for _, it := range items {
			cm := Comment{
				ID: it.GetID(), Body: it.GetBody(), URL: it.GetHTMLURL(),
				Path: it.GetPath(), Line: it.GetLine(), ReplyTo: it.GetInReplyTo(),
			}
			if u := it.GetUser(); u != nil {
				cm.Author = u.GetLogin()
			}
			if p := it.CreatedAt.GetTime(); p != nil {
				cm.When = p.Format("2006-01-02 15:04")
			}
			out = append(out, cm)
		}
		return resp, nil
	})
	return out, err
}

// CreatePRCommentOptions describes a review comment.
type CreatePRCommentOptions struct {
	Number int
	Body   string
	// Path and Line point at code. Both empty makes it a conversation comment instead.
	Path string
	Line int
	// Side is "RIGHT" (the new file) or "LEFT" (the old one). Defaults to RIGHT, which is what a
	// reviewer commenting on the change means.
	Side string
}

// CreatePRComment adds a comment to a pull request, either on the conversation or on a line.
//
// One method for both shapes because GitHub models them as one resource; which one you get
// depends on whether Path is set. The caller decides with a single argument instead of picking
// between two tools that behave the same way otherwise.
func (c *Client) CreatePRComment(ctx context.Context, o CreatePRCommentOptions) (Comment, error) {
	if o.Body == "" {
		return Comment{}, fmt.Errorf("a comment body is required")
	}
	if o.Path != "" && o.Line <= 0 {
		return Comment{}, fmt.Errorf("a line comment needs a line number: path %q was given without one", o.Path)
	}
	if o.Path == "" && o.Line > 0 {
		return Comment{}, fmt.Errorf("a line number only makes sense with a path; give both or neither")
	}

	// A line comment requires the commit being commented on, which is the PR's head.
	if o.Path != "" {
		pr, resp, err := c.gh.PullRequests.Get(ctx, c.Owner, c.Repo, o.Number)
		c.note(resp)
		if err != nil {
			return Comment{}, wrap(err)
		}
		side := o.Side
		if side == "" {
			side = "RIGHT"
		}
		line := o.Line
		req := gogithub.CreatePullRequestCommentRequest{
			Body:     o.Body,
			CommitID: pr.GetHead().GetSHA(),
			Path:     o.Path,
			Line:     &line,
			Side:     &side,
		}
		it, resp2, err := c.gh.PullRequests.CreateComment(ctx, c.Owner, c.Repo, o.Number, req)
		c.note(resp2)
		if err != nil {
			return Comment{}, wrap(err)
		}
		cm := Comment{ID: it.GetID(), Body: it.GetBody(), URL: it.GetHTMLURL(),
			Path: it.GetPath(), Line: it.GetLine()}
		if p := it.CreatedAt.GetTime(); p != nil {
			cm.When = p.Format("2006-01-02 15:04")
		}
		if u := it.GetUser(); u != nil {
			cm.Author = u.GetLogin()
		}
		return cm, nil
	}
	return c.CreateIssueComment(ctx, o.Number, o.Body)
}
