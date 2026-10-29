package ghclient

import (
	"context"
	"strings"

	"github.com/easydev-org/easyagent-git-ext/internal/uihandler"
)

// PullSource reads pull requests for the UI's PR page.
//
// The UI needs a few facts per page and a TOKEN per tenant, so this takes a function that resolves
// the tenant's token rather than holding one: a configuration change then applies to the next
// request instead of requiring a restart, and nothing here caches a credential.
type PullSource struct {
	// TokenFor returns the tenant's GitHub token, or "" when none is configured.
	TokenFor func(tenant string) string
	// TenantFrom resolves the caller for a request. The UI handlers authenticate before calling in,
	// but a source is a plain value and must not assume it.
	TenantFrom func(ctx context.Context) (string, error)
}

// client builds a client for one repository and tenant.
func (p *PullSource) client(ctx context.Context, repo string) (*Client, error) {
	tenant := ""
	if p.TenantFrom != nil {
		t, err := p.TenantFrom(ctx)
		if err != nil {
			return nil, err
		}
		tenant = t
	}
	token := ""
	if p.TokenFor != nil {
		token = strings.TrimSpace(p.TokenFor(tenant))
	}
	return New(Options{Remote: "https://github.com/" + strings.Trim(repo, "/"), Token: token})
}

// Pull returns the pull request's own facts.
func (p *PullSource) Pull(ctx context.Context, repo string, number int) (uihandler.PullView, error) {
	c, err := p.client(ctx, repo)
	if err != nil {
		return uihandler.PullView{}, err
	}
	pr, err := c.PR(ctx, number)
	if err != nil {
		return uihandler.PullView{}, err
	}
	return uihandler.PullView{
		Number:  number,
		Title:   pr.Title,
		State:   pr.State,
		Author:  pr.Author,
		Body:    pr.Body,
		Head:    pr.Head,
		Base:    pr.Base,
		URL:     pr.HTMLURL,
		Draft:   pr.Draft,
		Merged:  pr.Merged,
		Created: pr.CreatedAt,
	}, nil
}

// Files returns what the pull request changes.
func (p *PullSource) Files(ctx context.Context, repo string, number int) ([]uihandler.FileView, error) {
	c, err := p.client(ctx, repo)
	if err != nil {
		return nil, err
	}
	files, err := c.PRFiles(ctx, number)
	if err != nil {
		return nil, err
	}
	out := make([]uihandler.FileView, 0, len(files))
	for _, f := range files {
		out = append(out, uihandler.FileView{
			Path:           f.Path,
			Status:         f.Status,
			Additions:      f.Additions,
			Deletions:      f.Deletions,
			Patch:          f.Patch,
			PatchTruncated: f.PatchTruncated,
		})
	}
	return out, nil
}

// Diff returns the whole change as one unified diff.
func (p *PullSource) Diff(ctx context.Context, repo string, number int) (string, error) {
	c, err := p.client(ctx, repo)
	if err != nil {
		return "", err
	}
	return c.PRDiff(ctx, number)
}

// Checks returns the CI status.
func (p *PullSource) Checks(ctx context.Context, repo string, number int) (uihandler.ChecksView, error) {
	c, err := p.client(ctx, repo)
	if err != nil {
		return uihandler.ChecksView{}, err
	}
	ch, err := c.PRChecks(ctx, number)
	if err != nil {
		return uihandler.ChecksView{}, err
	}
	// `State()` is a METHOD: it reduces runs and statuses to one word. Reading a field of that name
	// would silently give the zero value.
	out := uihandler.ChecksView{State: ch.State(), HeadSHA: ch.HeadSHA}
	for _, run := range ch.Runs {
		out.Checks = append(out.Checks, uihandler.CheckRow{Name: run.Name, Status: run.Conclusion})
	}
	for _, st := range ch.Statuses {
		out.Checks = append(out.Checks, uihandler.CheckRow{Name: st.Context, Status: st.State})
	}
	return out, nil
}
