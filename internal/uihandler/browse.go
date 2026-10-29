package uihandler

import "context"

/**
 * BrowseSource answers "what is there" — the layer ABOVE a repository.
 *
 * WHY IT EXISTS: every other page here needs a repository to name, and until this there was no way
 * to arrive at one. Opening the panel gave a page that said "open a repository with #/files/…" and
 * nothing to click, which is a place with no door.
 *
 * These are ACCOUNT-level reads: which repositories, which organisations, and — once a repository is
 * chosen — its releases and workflows. They share an interface because they share a client and a
 * token and differ only in what they ask for.
 */
type BrowseSource interface {
	// Repos lists repositories the caller can see; `org` narrows to one organisation.
	Repos(ctx context.Context, org string) ([]RepoSummary, error)
	// Orgs lists the organisations the caller belongs to.
	Orgs(ctx context.Context) ([]OrgSummary, error)
	// Releases lists a repository's releases, newest first.
	Releases(ctx context.Context, repo string, limit int) ([]ReleaseSummary, error)
	// Workflows lists a repository's workflows.
	Workflows(ctx context.Context, repo string) ([]WorkflowSummary, error)
	// Whoami names the authenticated account, so the panel can say whose view this is.
	Whoami(ctx context.Context) (string, error)
}

// RepoSummary is one repository in a list.
type RepoSummary struct {
	FullName      string `json:"fullName"`
	Description   string `json:"description"`
	Private       bool   `json:"private"`
	Archived      bool   `json:"archived"`
	Fork          bool   `json:"fork"`
	DefaultBranch string `json:"defaultBranch"`
}

// OrgSummary is one organisation.
type OrgSummary struct {
	Login       string `json:"login"`
	Description string `json:"description"`
}

// ReleaseSummary is one release as a list shows it. The body is included because a release IS its
// notes: a list of tag names alone would send the reader to GitHub to find out what shipped.
type ReleaseSummary struct {
	Tag         string `json:"tag"`
	Name        string `json:"name"`
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
	PublishedAt string `json:"publishedAt"`
	URL         string `json:"url"`
	Body        string `json:"body"`
	Assets      int    `json:"assets"`
}

// WorkflowSummary is one workflow definition.
type WorkflowSummary struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Path  string `json:"path"`
	State string `json:"state"`
}
