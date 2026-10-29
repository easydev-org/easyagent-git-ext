package ghclient

import (
	"context"
	"strings"

	"github.com/easydev-org/easyagent-gitdev-ext/internal/uihandler"
)

// IssueSource reads issues for the UI's issue page.
//
// SAME SHAPE AS PullSource, deliberately: one token resolver, one tenant lookup, and a client built
// per request so a configuration change applies to the next request instead of a restart. Two
// sources with two shapes would be two things to keep right for no gain.
type IssueSource struct {
	// TokenFor returns the tenant's GitHub token, or "" when none is configured.
	TokenFor func(tenant string) string
	// TenantFrom resolves the caller for a request.
	TenantFrom func(ctx context.Context) (string, error)
}

func (s *IssueSource) client(ctx context.Context, repo string) (*Client, error) {
	tenant := ""
	if s.TenantFrom != nil {
		t, err := s.TenantFrom(ctx)
		if err != nil {
			return nil, err
		}
		tenant = t
	}
	token := ""
	if s.TokenFor != nil {
		token = strings.TrimSpace(s.TokenFor(tenant))
	}
	return New(Options{Remote: "https://github.com/" + strings.Trim(repo, "/"), Token: token})
}

// Issue returns the issue's own facts.
//
// A PULL REQUEST IS REPORTED AS SUCH rather than refused: GitHub numbers both in one sequence, so a
// reader following a link can land here on a pull request. Saying so lets the page offer the code
// instead of showing an issue page that silently has none.
func (s *IssueSource) Issue(ctx context.Context, repo string, number int) (uihandler.IssueView, error) {
	c, err := s.client(ctx, repo)
	if err != nil {
		return uihandler.IssueView{}, err
	}
	is, err := c.Issue(ctx, number)
	if err != nil {
		return uihandler.IssueView{}, err
	}
	return uihandler.IssueView{
		Number:        is.Number,
		Title:         is.Title,
		State:         is.State,
		Body:          is.Body,
		Author:        is.Author,
		URL:           is.HTMLURL,
		Labels:        is.Labels,
		IsPullRequest: is.IsPullRequest,
	}, nil
}

// Comments returns the issue's conversation, oldest first.
func (s *IssueSource) Comments(ctx context.Context, repo string, number int) ([]uihandler.CommentView, error) {
	c, err := s.client(ctx, repo)
	if err != nil {
		return nil, err
	}
	list, err := c.IssueComments(ctx, number)
	if err != nil {
		return nil, err
	}
	out := make([]uihandler.CommentView, 0, len(list))
	for _, cm := range list {
		out = append(out, uihandler.CommentView{
			ID: cm.ID, Author: cm.Author, Body: cm.Body, When: cm.When,
		})
	}
	return out, nil
}
