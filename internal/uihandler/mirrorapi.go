package uihandler

import (
	"net/http"
	"strings"

	"github.com/easydev-org/easyagent-gitdev-ext/internal/forge"
)

// The mirror's file-browsing endpoints.
//
// WHY THEY LIVE HERE NOW: they used to be their own extension, which owned the mirror side while
// this one owned GitHub. The two are one extension, so a panel that browses the mirror belongs here
// — otherwise the only way to look at a file in the mirror is a tool call, and a reader with no tool
// in hand has no way in.
//
// THE MERGE-REQUEST ENDPOINTS DID NOT COME. Merge requests are reviewed on the UPSTREAM; a mirror
// that also listed its own would be a second place where work appears to be discussed and merged,
// with two answers to "was this merged?".

// mirrorRepo resolves the forge client for the repository named in the query, or writes the error.
//
// Authentication comes FIRST and before the repository is even parsed: an unauthenticated caller
// must not learn whether a repository exists. The tenant comes from the TOKEN and the session from
// the query, so a session can narrow a tenant's view but never widen it.
func (h *Handler) mirrorRepo(w http.ResponseWriter, r *http.Request) (*forge.Client, bool) {
	tenant, err := h.who(r)
	if err != nil {
		writeErr(w, err)
		return nil, false
	}
	repo := strings.TrimSpace(r.URL.Query().Get("repo"))
	if repo == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo (owner/name) is required"})
		return nil, false
	}
	c, ferr := h.Forge(r.Context(), r.URL.Query().Get("session"), tenant, repo)
	if ferr != nil {
		// A forge that cannot be reached is not an authentication failure: reporting 401 would log
		// the reader out over a network fault.
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": ferr.Error()})
		return nil, false
	}
	return c, true
}
