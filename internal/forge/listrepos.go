package forge

import (
	"context"
	"fmt"
	"net/http"
)

/**
 * ListRepos returns every repository on the forge this client is configured for.
 *
 * IT LISTS THE FORGE, not one owner: the panel needs to know which of the names IT has are present
 * here, and the owners vary. Pagination stops when a short page arrives, so a small install costs one
 * request.
 *
 * The client's own `path()` names a repository and cannot be reused — this call is above that level.
 */
func (c *Client) ListRepos(ctx context.Context) ([]RepoOnForge, error) {
	const perPage = 50
	// A CEILING, because a runaway loop against a large forge would hold the request open until the
	// caller's deadline with nothing to show for it. Twenty pages is a thousand repositories, which
	// is far more than a panel lists.
	const maxPages = 20

	out := make([]RepoOnForge, 0, perPage)
	for page := 1; page <= maxPages; page++ {
		var got struct {
			Data []struct {
				FullName string `json:"full_name"`
				// Empty marks a repository with no commits. It EXISTS and answers 200, but has no
				// branches and no tree — so "is it there" and "can it be browsed" are different
				// questions, and this is the field that tells them apart.
				Empty bool `json:"empty"`
			} `json:"data"`
		}
		apiPath := fmt.Sprintf("/repos/search?limit=%d&page=%d", perPage, page)
		if err := c.do(ctx, http.MethodGet, apiPath, nil, &got); err != nil {
			return nil, err
		}
		for _, r := range got.Data {
			out = append(out, RepoOnForge{FullName: r.FullName, Empty: r.Empty})
		}
		if len(got.Data) < perPage {
			break
		}
	}
	return out, nil
}

// RepoOnForge is one repository as the forge reports it.
type RepoOnForge struct {
	FullName string
	// Empty is true when the repository has no commits. It is not the same as "does not exist": an
	// empty repository is real, writable, and has nothing to show.
	Empty bool
}
