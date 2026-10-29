package forge

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

/**
 * CreateRepo makes a repository on the forge, with an initial commit.
 *
 * INITIALISED, ALWAYS, and that is not a detail. A repository created without a commit is REAL — it
 * answers 200 and is writable in principle — but the contents API refuses it with an empty 500, as
 * does every tree and branch call. Every later step of establishing a mirror starts by writing to
 * it, so an empty repository is a dead end that looks alive.
 *
 * IDEMPOTENT: a repository that already exists is reported as such rather than as a failure, because
 * the caller's intent is "make sure this exists" and a name collision is the answer they wanted.
 */
func (c *Client) CreateRepo(ctx context.Context, name string, private bool) (existed bool, err error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return false, fmt.Errorf("name is required")
	}
	err = c.do(ctx, http.MethodPost, "/user/repos", map[string]any{
		"name":      name,
		"private":   private,
		"auto_init": true,
	}, nil)
	if err == nil {
		return false, nil
	}
	// A name that is taken answers 409 — which is exactly what a caller who said "make sure this
	// exists" wanted, so it is reported as success-with-a-fact rather than as a failure.
	if strings.Contains(err.Error(), "409") {
		return true, nil
	}
	return false, err
}
