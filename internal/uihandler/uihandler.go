// Package uihandler serves the browser panel's pull-request data.
//
// WHAT IS LEFT OF IT: the pull-request endpoints. Everything else — the file tree, diffs, logs, the
// workspace picker — read a LOCAL CHECKOUT that this extension no longer keeps. Repository content
// now lives on the forge and is read there (see the gitea extension), so those endpoints had no data
// source and were removed with the workspace they read.
//
// What remains is genuinely GitHub-side: a pull request, its files, its diff and its CI status exist
// only on the remote, and fetching them here is not a second copy of anything.
package uihandler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// Handler answers the UI's JSON requests.
type Handler struct {
	// Identity resolves the caller's bearer to a tenant. REQUIRED: without it every request is
	// refused, because the alternative is trusting a `?tenant=` parameter.
	Identity *IdentityResolver
	// Pulls reads pull requests from GitHub. Optional: a deployment without a GitHub token simply
	// has no PR pages, rather than failing at call time.
	Pulls PullSource
}

// who resolves the caller's tenant, or an error the handler must turn into 401.
//
// TENANT IS NOT TAKEN FROM THE REQUEST. It is resolved from the bearer token, because a query
// parameter is caller-controlled: `?tenant=victim` would otherwise read another tenant's data. That
// was a real hole, and it is why `who()` exists.
func (h *Handler) who(r *http.Request) (string, error) {
	if h.Identity == nil {
		// Failing closed: a deployment that forgot to wire identity must not serve data.
		return "", errors.New("identity is not configured")
	}
	b := bearerOf(r)
	return h.Identity.Tenant(r.Context(), b)
}

// bearerOf reads the Authorization header, tolerating the case variations proxies introduce.
func bearerOf(r *http.Request) string {
	v := r.Header.Get("Authorization")
	if len(v) > 7 && strings.EqualFold(v[:7], "bearer ") {
		return strings.TrimSpace(v[7:])
	}
	return ""
}

/**
 * Register mounts the panel's API under `prefix`.
 *
 * The prefix is part of every route rather than stripped by the caller: a host that proxies this
 * mount hands the handler the request it received, path intact. Registering bare "/pull" would never
 * match "/ext/github/api/pull" — a mistake that cost a whole debugging session on another extension.
 */
func (h *Handler) Register(mux *http.ServeMux, prefix string) {
	p := strings.TrimSuffix(prefix, "/") + "/api/"
	mux.HandleFunc(p+"pull", h.pull)
	mux.HandleFunc(p+"pull/files", h.pullFiles)
	mux.HandleFunc(p+"pull/diff", h.pullDiff)
	mux.HandleFunc(p+"pull/checks", h.pullChecks)
}

// tenantKey carries the resolved tenant through a request's context, so a PullSource — which takes a
// plain context — can resolve the token without re-authenticating. The pull handlers put it there
// after `who()` has succeeded, and nothing else writes it: a value a caller could set would be a
// value a caller could choose.
type tenantKey struct{}

// tenantFrom reads the tenant the handler resolved.
func tenantFrom(ctx context.Context) (string, error) {
	if t, ok := ctx.Value(tenantKey{}).(string); ok && t != "" {
		return t, nil
	}
	return "", errors.New("no tenant on the request context")
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeErr(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, ErrUnauthenticated) {
		status = http.StatusUnauthorized
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
