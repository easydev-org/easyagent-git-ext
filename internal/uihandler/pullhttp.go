package uihandler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
)

// tenantKey carries the resolved tenant through a request's context, so a PullSource — which takes a
// plain context — can resolve the token without re-authenticating. The handlers below put it there
// after `who()` has succeeded, and nothing else writes it: a value that a caller could set would be
// a value a caller could choose.
type tenantKey struct{}

// tenantFrom reads the tenant the handler resolved.
func tenantFrom(ctx context.Context) (string, error) {
	if t, ok := ctx.Value(tenantKey{}).(string); ok && t != "" {
		return t, nil
	}
	return "", errors.New("no tenant on the request context")
}

// The pull-request endpoints behind the UI's PR page.
//
// Split from uihandler.go because they are the opposite kind of read: everything there is the LOCAL
// checkout, everything here is GitHub. Keeping them in one file invites a caller to assume the auth
// and caching story is the same, and it is not — these need a token and they cost rate limit.

// authedPull resolves the tenant and the request's repository/number, or writes the error.
func (h *Handler) authedPull(w http.ResponseWriter, r *http.Request) (repo string, number int, ok bool) {
	// Authentication FIRST, before anything is parsed: an unauthenticated caller must not learn
	// whether a repository or pull request exists.
	tenant, err := h.who(r)
	if err != nil {
		writeErr(w, err)
		return "", 0, false
	}
	if h.Pulls == nil {
		writeErr(w, errors.New("pull requests are not configured"))
		return "", 0, false
	}
	// Carry it on the request so the source can resolve the tenant's token without a second lookup.
	*r = *r.WithContext(context.WithValue(r.Context(), tenantKey{}, tenant))
	q := r.URL.Query()
	repo = q.Get("repo")
	if repo == "" {
		writeErr(w, errors.New("repo (owner/name) is required"))
		return "", 0, false
	}
	n, err := strconv.Atoi(q.Get("number"))
	if err != nil || n <= 0 {
		writeErr(w, errors.New("number (the pull request id) is required"))
		return "", 0, false
	}
	return repo, n, true
}

func (h *Handler) pull(w http.ResponseWriter, r *http.Request) {
	repo, n, ok := h.authedPull(w, r)
	if !ok {
		return
	}
	v, err := h.Pulls.Pull(r.Context(), repo, n)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (h *Handler) pullFiles(w http.ResponseWriter, r *http.Request) {
	repo, n, ok := h.authedPull(w, r)
	if !ok {
		return
	}
	v, err := h.Pulls.Files(r.Context(), repo, n)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": v, "count": len(v)})
}

func (h *Handler) pullDiff(w http.ResponseWriter, r *http.Request) {
	repo, n, ok := h.authedPull(w, r)
	if !ok {
		return
	}
	text, err := h.Pulls.Diff(r.Context(), repo, n)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"diff": text})
}

func (h *Handler) pullChecks(w http.ResponseWriter, r *http.Request) {
	repo, n, ok := h.authedPull(w, r)
	if !ok {
		return
	}
	v, err := h.Pulls.Checks(r.Context(), repo, n)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
