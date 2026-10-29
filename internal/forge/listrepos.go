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
func (c *Client) ListRepos(ctx context.Context) ([]string, error) {
	const perPage = 50
	// A CEILING, because a runaway loop against a large forge would hold the request open until the
	// caller's deadline with nothing to show for it. Twenty pages is a thousand repositories, which
	// is far more than a panel lists.
	const maxPages = 20

	out := make([]string, 0, perPage)
	for page := 1; page <= maxPages; page++ {
		var got struct {
			Data []struct {
				FullName string `json:"full_name"`
			} `json:"data"`
		}
		apiPath := fmt.Sprintf("/repos/search?limit=%d&page=%d", perPage, page)
		if err := c.do(ctx, http.MethodGet, apiPath, nil, &got); err != nil {
			return nil, err
		}
		for _, r := range got.Data {
			out = append(out, r.FullName)
		}
		if len(got.Data) < perPage {
			break
		}
	}
	return out, nil
}
