// Package uihandler serves the browser panel's data: the MIRROR's files and the UPSTREAM's pull
// requests.
//
// TWO SOURCES, ONE PANEL, because they are two halves of one job. The mirror is where a change is
// made, so its files are what a reader wants while writing one; the pull request is where the change
// is reviewed, so that is what they want afterwards. A panel that showed only one of them would
// send the reader to a second extension mid-task.
//
// NOTHING HERE WRITES. Browsing and reviewing are reads; every change goes through a tool, where the
// permission model already lives. A page that could merge would need its own permission story and
// does not have one.
package uihandler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/easydev-org/easyagent-gitdev-ext/internal/forge"
)

// Handler answers the UI's JSON requests.
type Handler struct {
	// Identity resolves the caller's bearer to a tenant. REQUIRED: without it every request is
	// refused, because the alternative is trusting a `?tenant=` parameter.
	Identity *IdentityResolver
	// Pulls reads pull requests from GitHub. Optional: a deployment without a GitHub token simply
	// has no PR pages, rather than failing at call time.
	Pulls PullSource
	// Forge builds a client for one MIRROR repository in the caller's scope. Optional: a deployment
	// with no mirror configured has no file-browsing pages.
	//
	// A FUNCTION rather than a client because the token and the repository come from extension
	// CONFIG, which can change while this process runs, and because the client is per-tenant.
	Forge MirrorForgeFunc
}

// MirrorForgeFunc builds a forge client for one repository in one caller's scope.
type MirrorForgeFunc func(ctx context.Context, session, tenant, repo string) (*forge.Client, error)

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
 * match "/ext/gitdev/api/pull" — a mistake that cost a whole debugging session on another extension.
 */
func (h *Handler) Register(mux *http.ServeMux, prefix string) {
	p := strings.TrimSuffix(prefix, "/") + "/api/"
	mux.HandleFunc(p+"pull", h.pull)
	mux.HandleFunc(p+"pull/files", h.pullFiles)
	mux.HandleFunc(p+"pull/diff", h.pullDiff)
	mux.HandleFunc(p+"pull/checks", h.pullChecks)

	// The MIRROR's own pages: browse the repository a change is being made in.
	//
	// Registered here rather than in a separate extension because the panel is one place: a reader
	// writing a change looks at a file, and a reader reviewing one looks at a pull request, and they
	// are the same person a minute apart.
	mux.HandleFunc(p+"repo", h.repo)
	mux.HandleFunc(p+"tree", h.tree)
	mux.HandleFunc(p+"file", h.file)
	mux.HandleFunc(p+"refs", h.refs)
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
