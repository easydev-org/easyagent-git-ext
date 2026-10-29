package uihandler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
)

// The CI run endpoints.
//
// A run has THREE levels — the repository's runs, one run's jobs, one job's log — and only the first
// two are cheap. The third is a separate route because a log is megabytes: fetching it with the page
// would make opening the page cost the whole log whether or not anybody read it.

// authedRun resolves the tenant, the repository and the numeric id, or writes the error.
//
// Authentication comes FIRST and before the id is parsed: an unauthenticated caller must not learn
// whether a run exists.
func (h *Handler) authedRun(w http.ResponseWriter, r *http.Request, param string) (repo string, id int64, ok bool) {
	tenant, err := h.who(r)
	if err != nil {
		writeErr(w, err)
		return "", 0, false
	}
	// Carry it on the request so the source can resolve the tenant's token without a second lookup.
	*r = *r.WithContext(context.WithValue(r.Context(), tenantKey{}, tenant))

	q := r.URL.Query()
	repo = q.Get("repo")
	if repo == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo (owner/name) is required"})
		return "", 0, false
	}
	id, cerr := strconv.ParseInt(q.Get(param), 10, 64)
	if cerr != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": param + " is required"})
		return "", 0, false
	}
	return repo, id, true
}

func (h *Handler) runs(w http.ResponseWriter, r *http.Request) {
	tenant, err := h.who(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	*r = *r.WithContext(context.WithValue(r.Context(), tenantKey{}, tenant))

	repo := r.URL.Query().Get("repo")
	if repo == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo (owner/name) is required"})
		return
	}
	limit := 20
	if v, cerr := strconv.Atoi(r.URL.Query().Get("limit")); cerr == nil && v > 0 && v <= 100 {
		limit = v
	}
	list, lerr := h.Runs.Runs(r.Context(), repo, limit)
	if lerr != nil {
		writeErr(w, lerr)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": list, "count": len(list)})
}

func (h *Handler) run(w http.ResponseWriter, r *http.Request) {
	repo, id, ok := h.authedRun(w, r, "run")
	if !ok {
		return
	}
	v, err := h.Runs.Run(r.Context(), repo, id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (h *Handler) runJobs(w http.ResponseWriter, r *http.Request) {
	repo, id, ok := h.authedRun(w, r, "run")
	if !ok {
		return
	}
	list, err := h.Runs.Jobs(r.Context(), repo, id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": list, "count": len(list)})
}

/**
 * runJobLog returns ONE job's log.
 *
 * IT IS ITS OWN REQUEST because that is the whole point: the page lists jobs without their logs, and
 * the reader opens the one they care about. A log that is not there yet — a job still running — is
 * reported as such rather than as an empty 200, so the UI can say "not yet" instead of "nothing".
 */
func (h *Handler) runJobLog(w http.ResponseWriter, r *http.Request) {
	repo, jobID, ok := h.authedRun(w, r, "job")
	if !ok {
		return
	}
	text, err := h.Runs.JobLog(r.Context(), repo, jobID)
	if err != nil {
		if errors.Is(err, ErrLogNotReady) {
			// 409: the request is valid, the resource is not ready — which a reader can act on
			// ("wait", or "reload") where a 500 would just look broken.
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"log": text, "bytes": len(text)})
}

// ErrLogNotReady means the job has not finished, so GitHub has published no log for it.
var ErrLogNotReady = errors.New("this job has not finished, so its log is not available yet")
