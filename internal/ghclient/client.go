// Package ghclient wraps google/go-github with this extension's own vocabulary.
//
// WHY go-github rather than a hand-written client. The first version of this file was
// hand-written, on the reasoning that only a dozen endpoints were needed. That reasoning was
// wrong in a way that produced two real defects:
//
//   - PAGINATION. Every list endpoint returns at most 100 items per page. Six list methods each
//     needed their own paging loop, and the hand-written version returned only the first page —
//     silently truncating any organization with more than 100 repositories.
//   - RATE LIMITING. Anonymous access allows 60 requests per hour. The hand-written version had
//     no idea what its remaining budget was, so a session would simply start getting 403s that
//     read as "not found".
//
// Both are structural, not sloppiness: they are the multiplication of "how many list methods"
// by "how many paging/limit conventions", and reimplementing them is how this class of bug
// keeps happening. go-github already solves both.
//
// The wrapper exists so the TOOL layer sees stable, small types (a Release, a PullRequest) and
// is not exposed to go-github's major-version churn — that project ships a new major almost
// monthly.
package ghclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	gogithub "github.com/google/go-github/v93/github"
)

// Client talks to GitHub for one repository (or none, for account-level calls).
type Client struct {
	Owner string
	Repo  string
	// Token may be empty: every READ endpoint this extension uses works anonymously on a
	// public repository, which is the point of the extension.
	Token string
	gh    *gogithub.Client
	// lastRate is what GitHub reported on the most recent call, kept so a caller can pace
	// itself. go-github returns this per Response; there is no client-level accessor, so it is
	// recorded here.
	lastRate gogithub.Rate
	hasRate  bool
}

// Options builds a Client.
type Options struct {
	// Remote is https://github.com/owner/repo(.git). May be empty for account-level clients
	// that only list organizations or packages.
	Remote  string
	Token   string
	BaseURL string // GitHub Enterprise API root; empty = api.github.com
	Timeout time.Duration
}

// New builds a client, parsing the remote when one is given.
func New(o Options) (*Client, error) {
	c := &Client{Token: o.Token}
	if strings.TrimSpace(o.Remote) != "" {
		owner, repo, err := SplitRemote(o.Remote)
		if err != nil {
			return nil, err
		}
		c.Owner, c.Repo = owner, repo
	}

	timeout := o.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	// Functional options, and an explicit timeout: go-github's default client has none, so a
	// hung connection would hold a tool call open until the turn's own deadline.
	opts := []gogithub.ClientOptionsFunc{gogithub.WithTimeout(timeout)}
	if o.BaseURL != "" {
		opts = append(opts, gogithub.WithEnterpriseURLs(strings.TrimSuffix(o.BaseURL, "/"),
			strings.TrimSuffix(o.BaseURL, "/")))
	}
	if o.Token != "" {
		opts = append(opts, gogithub.WithAuthToken(o.Token))
	}
	gh, err := gogithub.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("github client: %w", err)
	}
	c.gh = gh
	return c, nil
}

// SplitRemote reduces a GitHub remote to owner and repo.
//
// Accepts the three shapes a remote takes. Refusing an unrecognised one matters: a wrong
// owner/repo would silently address somebody else's repository.
func SplitRemote(remote string) (owner, repo string, err error) {
	s := strings.TrimSpace(remote)
	s = strings.TrimSuffix(s, ".git")
	if s == "" {
		return "", "", fmt.Errorf("empty remote")
	}
	// git@github.com:owner/repo
	if i := strings.Index(s, ":"); i >= 0 && strings.Contains(s[:i], "@") && !strings.Contains(s, "://") {
		s = s[i+1:]
	} else if strings.Contains(s, "://") {
		u, uerr := url.Parse(s)
		if uerr != nil {
			return "", "", fmt.Errorf("parse remote %q: %w", remote, uerr)
		}
		s = strings.TrimPrefix(u.Path, "/")
	}
	parts := strings.Split(strings.Trim(s, "/"), "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("remote %q does not look like owner/repo", remote)
	}
	owner, repo = parts[len(parts)-2], parts[len(parts)-1]
	if owner == "" || repo == "" {
		return "", "", fmt.Errorf("remote %q does not look like owner/repo", remote)
	}
	return owner, repo, nil
}

// Slug is "owner/repo".
func (c *Client) Slug() string { return c.Owner + "/" + c.Repo }

// wrap turns a go-github error into one that names the cause a caller can act on.
//
// go-github already distinguishes rate limiting from a plain 403; this projects that onto
// three answers this extension cares about. A missing distinction here is what makes a
// rate-limited session look like a permissions problem.
func wrap(err error) error {
	if err == nil {
		return nil
	}
	var rl *gogithub.RateLimitError
	if errors.As(err, &rl) {
		return fmt.Errorf("github rate limit reached (anonymous access allows 60 requests/hour); resets at %s — set a token to raise it: %w",
			rl.Rate.Reset.Time.Format(time.RFC3339), err)
	}
	var abuse *gogithub.AbuseRateLimitError
	if errors.As(err, &abuse) {
		return fmt.Errorf("github secondary rate limit (too many requests in a burst); retry shortly: %w", err)
	}
	return err
}

// IsNotFound reports whether err is a 404 from GitHub.
func IsNotFound(err error) bool {
	var ge *gogithub.ErrorResponse
	return errors.As(err, &ge) && ge.Response != nil && ge.Response.StatusCode == http.StatusNotFound
}

// IsUnauthorized reports a 401/403 that is NOT a rate limit — a bad or under-scoped token.
func IsUnauthorized(err error) bool {
	var rl *gogithub.RateLimitError
	var abuse *gogithub.AbuseRateLimitError
	if errors.As(err, &rl) || errors.As(err, &abuse) {
		return false
	}
	var ge *gogithub.ErrorResponse
	if !errors.As(err, &ge) || ge.Response == nil {
		return false
	}
	return ge.Response.StatusCode == http.StatusUnauthorized || ge.Response.StatusCode == http.StatusForbidden
}

// RateBudget reports the remaining anonymous/authenticated request budget, when GitHub
// reported it on the last call.
//
// Exposed because "how many calls do I have left" is the difference between a caller pacing
// itself and one that trips the limit mid-task.
func (c *Client) RateBudget() (remaining int, reset time.Time, ok bool) {
	if !c.hasRate {
		return 0, time.Time{}, false
	}
	return c.lastRate.Remaining, c.lastRate.Reset.Time, true
}

// note records the rate budget from a response, when there is one.
func (c *Client) note(resp *gogithub.Response) {
	if resp == nil || resp.Rate.Limit == 0 {
		return
	}
	c.lastRate = resp.Rate
	c.hasRate = true
}

// paginate calls fn for each page until GitHub stops offering one.
//
// Every list in this package goes through here. That is the whole reason for the wrapper: one
// paging loop instead of six, and no way to add a list method that quietly returns page one.
func paginate(fn func(page int) (*gogithub.Response, error)) error {
	page := 0
	for {
		resp, err := fn(page)
		if err != nil {
			return wrap(err)
		}
		if resp == nil || resp.NextPage == 0 {
			return nil
		}
		page = resp.NextPage
	}
}

var _ = context.Background
