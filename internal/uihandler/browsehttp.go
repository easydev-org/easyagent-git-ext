package uihandler

import (
	"context"
	"net/http"
	"strconv"
)

// The account-level endpoints: which repositories, which organisations, and a repository's releases
// and workflows.
//
// NONE OF THESE TAKE A `repo` EXCEPT THE LAST TWO, which is the whole point: they are how a reader
// arrives at a repository in the first place.

// authedAccount authenticates and puts the tenant on the request, for the endpoints that need no
// repository.
func (h *Handler) authedAccount(w http.ResponseWriter, r *http.Request) bool {
	tenant, err := h.who(r)
	if err != nil {
		writeErr(w, err)
		return false
	}
	*r = *r.WithContext(context.WithValue(r.Context(), tenantKey{}, tenant))
	return true
}

func (h *Handler) repos(w http.ResponseWriter, r *http.Request) {
	if !h.authedAccount(w, r) {
		return
	}
	org := r.URL.Query().Get("org")
	list, err := h.Browse.Repos(r.Context(), org)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"repos": list, "count": len(list), "org": org})
}

func (h *Handler) orgs(w http.ResponseWriter, r *http.Request) {
	if !h.authedAccount(w, r) {
		return
	}
	list, err := h.Browse.Orgs(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"orgs": list, "count": len(list)})
}

func (h *Handler) whoami(w http.ResponseWriter, r *http.Request) {
	if !h.authedAccount(w, r) {
		return
	}
	login, err := h.Browse.Whoami(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"login": login})
}

func (h *Handler) releases(w http.ResponseWriter, r *http.Request) {
	if !h.authedAccount(w, r) {
		return
	}
	repo := r.URL.Query().Get("repo")
	if repo == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo (owner/name) is required"})
		return
	}
	limit := 20
	if v, cerr := strconv.Atoi(r.URL.Query().Get("limit")); cerr == nil && v > 0 && v <= 100 {
		limit = v
	}
	list, err := h.Browse.Releases(r.Context(), repo, limit)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"releases": list, "count": len(list)})
}

func (h *Handler) workflows(w http.ResponseWriter, r *http.Request) {
	if !h.authedAccount(w, r) {
		return
	}
	repo := r.URL.Query().Get("repo")
	if repo == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo (owner/name) is required"})
		return
	}
	list, err := h.Browse.Workflows(r.Context(), repo)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"workflows": list, "count": len(list)})
}
