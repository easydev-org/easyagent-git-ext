// Package uihandler serves the browser UI's data.
//
// EVERYTHING HERE READS THE LOCAL WORKSPACE, NOT GITHUB.
//
// A checkout is already on the pod's disk, so the file tree, file contents, status, diff, log and
// blame are plain disk and go-git reads. Going to api.github.com for those would be slower, rate
// limited (60 requests/hour unauthenticated, 5000 authenticated), and — for a repository the user
// just edited — WRONG, because the remote does not have those edits yet. The workspace is the only
// source that knows what this session changed.
//
// The one thing that cannot come from disk is the clone itself; that already happens through the
// `git-ws-clone` tool.
package uihandler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/easydev-org/easyagent-github-ext/internal/workspace"
)

// Handler answers the UI's JSON requests.
type Handler struct {
	Workspaces *workspace.Manager
}

// scope is the (tenant, session) pair that owns a workspace.
//
// The UI cannot invent it: Manager.Get refuses a workspace the caller does not own, and that check
// is the only thing standing between one session's checkout and another's. So the scope arrives as
// explicit query parameters and is passed through untouched — this layer never guesses it.
type scope struct {
	ws      string
	tenant  string
	session string
}

func scopeOf(r *http.Request) scope {
	q := r.URL.Query()
	return scope{ws: q.Get("ws"), tenant: q.Get("tenant"), session: q.Get("session")}
}

func (h *Handler) ws(s scope) (*workspace.Workspace, error) {
	if h.Workspaces == nil {
		return nil, errors.New("workspaces are not configured")
	}
	if s.ws == "" {
		return nil, errors.New("ws (workspace id) is required")
	}
	return h.Workspaces.Get(s.ws, s.tenant, s.session)
}

// writeJSON is the only way this package answers, so an error is always the same shape and the UI
// has one branch to handle instead of guessing per endpoint.
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	code := http.StatusBadRequest
	if errors.Is(err, workspace.ErrNoWorkspace) || errors.Is(err, workspace.ErrNotOwned) {
		// 404 rather than 403 for NotOwned: telling a caller that someone else's workspace exists is
		// information they do not need.
		code = http.StatusNotFound
	}
	writeJSON(w, code, map[string]any{"error": err.Error()})
}

// Register wires the UI's endpoints onto mux, under `prefix`.
//
// The prefix MUST be the same one the UI is mounted at (UISpec.Base), because the host proxies only
// that prefix to this extension. The API originally lived at /api/v1/ui/ — an absolute path that
// the UI, served from /ext/github/, then requested from the HOST's root, where it fell through to
// the host's SPA and returned HTML. Every call failed with a JSON parse error and no 404 to point
// at it.
//
// Keeping the API under the UI's own prefix also means one reverse-proxy rule covers both.
func (h *Handler) Register(mux *http.ServeMux, prefix string) {
	p := strings.TrimSuffix(prefix, "/") + "/api/"
	mux.HandleFunc(p+"workspaces", h.listWorkspaces)
	mux.HandleFunc(p+"tree", h.tree)
	mux.HandleFunc(p+"file", h.file)
	mux.HandleFunc(p+"status", h.status)
	mux.HandleFunc(p+"diff", h.diff)
	mux.HandleFunc(p+"log", h.log)
}

func (h *Handler) listWorkspaces(w http.ResponseWriter, r *http.Request) {
	if h.Workspaces == nil {
		writeErr(w, errors.New("workspaces are not configured"))
		return
	}
	q := r.URL.Query()
	list := h.Workspaces.List(q.Get("tenant"), q.Get("session"))
	out := make([]map[string]any, 0, len(list))
	for _, ws := range list {
		out = append(out, map[string]any{
			"id": ws.ID, "repo": ws.Repo, "branch": ws.CurrentBranch(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"workspaces": out})
}

// underDir returns f relative to dir, and whether f is inside it at all.
//
// Compared segment-wise rather than with strings.HasPrefix: "src-old/x" has the prefix "src" but is
// not inside "src", and a tree that showed it would let a click open a file the caller did not ask
// for.
func underDir(f, dir string) (string, bool) {
	dir = trimSlash(dir)
	if dir == "" {
		return f, true
	}
	if f == dir {
		return "", false
	}
	if len(f) > len(dir)+1 && f[:len(dir)] == dir && f[len(dir)] == '/' {
		return f[len(dir)+1:], true
	}
	return "", false
}

func trimSlash(p string) string {
	for len(p) > 0 && p[0] == '/' {
		p = p[1:]
	}
	for len(p) > 0 && p[len(p)-1] == '/' {
		p = p[:len(p)-1]
	}
	return p
}

// indexSep returns the index of the first path separator, or -1.
func indexSep(p string) int {
	for i := 0; i < len(p); i++ {
		if p[i] == '/' {
			return i
		}
	}
	return -1
}

// fileNode is one entry in the tree.
//
// Flat paths from ListFiles are turned into a tree here rather than in the browser: the UI renders
// a directory listing one level at a time, and doing that from a flat list client-side means
// re-deriving parents for every node on every render.
type fileNode struct {
	Path string `json:"path"`
	Dir  bool   `json:"dir"`
	Size int64  `json:"size,omitempty"`
}

func (h *Handler) tree(w http.ResponseWriter, r *http.Request) {
	ws, err := h.ws(scopeOf(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	files, err := ws.ListFiles()
	if err != nil {
		writeErr(w, err)
		return
	}
	// `dir` narrows to one directory's entries. Without it the whole tree is returned, which is
	// what a "show everything" toggle wants.
	dir := r.URL.Query().Get("dir")
	out := make([]fileNode, 0, len(files))
	seenDirs := map[string]bool{}
	for _, f := range files {
		if dir == "" {
			out = append(out, fileNode{Path: f})
			continue
		}
		rel, ok := underDir(f, dir)
		if !ok {
			continue
		}
		// Only the immediate children, and a directory appears once.
		if i := indexSep(rel); i >= 0 {
			d := dir + "/" + rel[:i]
			if !seenDirs[d] {
				seenDirs[d] = true
				out = append(out, fileNode{Path: trimSlash(dir) + "/" + rel[:i], Dir: true})
			}
			continue
		}
		out = append(out, fileNode{Path: trimSlash(dir) + "/" + rel})
	}
	writeJSON(w, http.StatusOK, map[string]any{"dir": dir, "entries": out})
}

func (h *Handler) file(w http.ResponseWriter, r *http.Request) {
	ws, err := h.ws(scopeOf(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	p := r.URL.Query().Get("path")
	if p == "" {
		writeErr(w, errors.New("path is required"))
		return
	}
	data, err := ws.ReadFile(p)
	if err != nil {
		writeErr(w, err)
		return
	}
	// Binary detection by NUL byte: a repository has images and archives, and shipping megabytes of
	// base64 into an editor pane helps nobody. The UI shows "binary" and offers a download instead.
	bin := false
	for _, b := range data {
		if b == 0 {
			bin = true
			break
		}
	}
	resp := map[string]any{"path": p, "size": len(data), "binary": bin}
	if !bin {
		resp["content"] = string(data)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	ws, err := h.ws(scopeOf(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	st, err := ws.Status()
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"branch": ws.CurrentBranch(), "changes": st})
}

func (h *Handler) diff(w http.ResponseWriter, r *http.Request) {
	ws, err := h.ws(scopeOf(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	q := r.URL.Query()
	text, err := ws.Diff(workspace.DiffOptions{
		Path:   q.Get("path"),
		Staged: q.Get("staged") == "1",
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"diff": text})
}

func (h *Handler) log(w http.ResponseWriter, r *http.Request) {
	ws, err := h.ws(scopeOf(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	// Bounded: an unbounded log of a large repository is megabytes for a panel that shows a dozen
	// rows.
	limit := 50
	if v, cerr := strconv.Atoi(r.URL.Query().Get("limit")); cerr == nil && v > 0 && v <= 500 {
		limit = v
	}
	entries, err := ws.Log(limit)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}
