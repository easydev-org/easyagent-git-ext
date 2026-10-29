package forge

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

/**
 * WhoAmI names the account the token belongs to.
 *
 * NEEDED BECAUSE REPOSITORIES HERE ARE CREATED UNDER THAT ACCOUNT. A mirror of `easydev-org/x` lands
 * at `agentdev/x`, and a caller that assumed the two names matched would navigate to a repository
 * that does not exist. The route has to be built from what this returns.
 */
func (c *Client) WhoAmI(ctx context.Context) (string, error) {
	var me struct {
		Login string `json:"login"`
	}
	if err := c.do(ctx, http.MethodGet, "/user", nil, &me); err != nil {
		return "", err
	}
	if strings.TrimSpace(me.Login) == "" {
		return "", fmt.Errorf("the forge did not name the account this token belongs to")
	}
	return me.Login, nil
}
