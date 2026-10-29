package uihandler

import (
	"context"
	"net/http"
	"strconv"
	"strings"
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

/**
 * mirrored reports which of the named repositories exist in the MIRROR.
 *
 * IT TAKES `repos` AS A REPEATED QUERY PARAMETER, so the home page can ask about everything it is
 * showing in one request. Asking per row would be eighty round trips to the forge for a page a reader
 * scrolls in two seconds.
 *
 * A repository that cannot be checked is reported as NOT mirrored rather than as an error: the
 * answer's job is to tell a reader which rows are clickable, and a whole page that fails because one
 * name was odd would be worse than one row marked wrong.
 */
func (h *Handler) mirrored(w http.ResponseWriter, r *http.Request) {
	if !h.authedAccount(w, r) {
		return
	}
	repos := r.URL.Query()["repo"]
	if len(repos) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"mirrored": map[string]bool{}})
		return
	}
	// Bounded: the page shows tens, and an unbounded list would let one request ask the forge about
	// thousands.
	if len(repos) > 200 {
		repos = repos[:200]
	}
	set, err := h.Mirrored.Mirrored(r.Context(), repos)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"mirrored": set})
}

/**
 * establish creates a mirror of one repository, from its upstream.
 *
 * A POST, because it changes things — it creates a repository on the forge and copies a branch into
 * it. The panel calls it when a reader opens a repository the mirror does not have, which is the
 * moment they have said they want it.
 *
 * SLOW BY NATURE: a large repository takes tens of seconds to fetch. The response says what was
 * copied rather than only that it worked, so the page can show the commit that arrived.
 */
func (h *Handler) establish(w http.ResponseWriter, r *http.Request) {
	if !h.authedAccount(w, r) {
		return
	}
	repo := strings.TrimSpace(r.URL.Query().Get("repo"))
	if repo == "" || !strings.Contains(repo, "/") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo (owner/name) is required"})
		return
	}
	mirroredAs, branch, commit, err := h.Mirrored.Establish(r.Context(), repo)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		// THE LOCATION THE PANEL MUST NAVIGATE TO, not the one it asked about: the mirror is created
		// under the forge account, which is not the upstream's owner.
		"repo": repo, "mirroredAs": mirroredAs, "branch": branch, "commit": commit,
	})
}
