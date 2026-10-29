package uihandler

import (
	"net/http"
	"strings"
)

// repoInfo describes the repository the panel is browsing.
//
// Echoed back rather than assumed: the panel is opened with a repository name in its own URL, and a
// mistyped or renamed one must be reported as such instead of rendering an empty tree.
func (h *Handler) repo(w http.ResponseWriter, r *http.Request) {
	c, ok := h.mirrorRepo(w, r)
	if !ok {
		return
	}
	refs, err := c.ListRefs(r.Context())
	if err != nil {
		writeForgeErr(w, err)
		return
	}
	// The default branch is the first "branch" the forge lists; a repository with none is empty, and
	// the panel says so rather than offering a branch picker with nothing in it.
	def := ""
	branches := make([]string, 0, len(refs))
	tags := make([]string, 0, len(refs))
	for _, ref := range refs {
		if ref.Kind == "branch" {
			branches = append(branches, ref.Name)
			if def == "" {
				def = ref.Name
			}
		} else {
			tags = append(tags, ref.Name)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"repo":          strings.Trim(r.URL.Query().Get("repo"), "/"),
		"defaultBranch": def,
		"branches":      branches,
		"tags":          tags,
	})
}

// refs lists what can be browsed: every branch and tag.
func (h *Handler) refs(w http.ResponseWriter, r *http.Request) {
	c, ok := h.mirrorRepo(w, r)
	if !ok {
		return
	}
	refs, err := c.ListRefs(r.Context())
	if err != nil {
		writeForgeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"refs": refs, "count": len(refs)})
}

// tree lists one directory.
func (h *Handler) tree(w http.ResponseWriter, r *http.Request) {
	c, ok := h.mirrorRepo(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	entries, err := c.ListDir(r.Context(), q.Get("dir"), q.Get("ref"))
	if err != nil {
		writeForgeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"dir": strings.Trim(q.Get("dir"), "/"), "ref": q.Get("ref"),
		"entries": entries, "count": len(entries),
	})
}

// file returns one file's text.
func (h *Handler) file(w http.ResponseWriter, r *http.Request) {
	c, ok := h.mirrorRepo(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	path := q.Get("path")
	if path == "" {
		writeMirrorErr(w, http.StatusBadRequest, "path is required")
		return
	}
	f, err := c.ReadFile(r.Context(), path, q.Get("ref"))
	if err != nil {
		writeForgeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path": f.Path, "revision": f.Revision, "content": f.Content, "size": f.Size,
	})
}

// writeMirrorErr answers with a specific status, for the cases where the reason is known here rather
// than discovered from a forge error.
func writeMirrorErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

/**
 * writeForgeErr maps a forge failure to something a browser can act on.
 *
 * A forge that is merely UNREACHABLE answers 502, not 500: it is a state of a remote dependency, and
 * a panel retries it rather than showing a permanent failure. A missing repository is 404 and a
 * refused read is 403, so the reader can tell "wrong path" from "no access" without reading the
 * message.
 */
func writeForgeErr(w http.ResponseWriter, err error) {
	msg := err.Error()
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "not found") || strings.Contains(low, "404"):
		writeMirrorErr(w, http.StatusNotFound, msg)
	case strings.Contains(low, "unauthorized") || strings.Contains(low, "401") ||
		strings.Contains(low, "forbidden") || strings.Contains(low, "403"):
		writeMirrorErr(w, http.StatusForbidden, msg)
	default:
		writeMirrorErr(w, http.StatusBadGateway, msg)
	}
}
