package tools

import (
	"net/url"
	"strings"
)

/**
 * mirrorView points at the MIRROR panel for one repository.
 *
 * The mirror is a separate extension from this one: it owns the branch-and-commit side, and its
 * panel browses files. A view emitted here therefore names THAT extension, not this one — a link
 * to a route this extension does not serve would open on nothing.
 *
 * WHY THE TOOL AND NOT THE MANIFEST: only the tool knows WHICH repository it just read. A merge
 * request listing can be asked about a remote, a checkout, or the session's own clone, and the
 * resolved `owner/name` exists in no argument and in no manifest — it is the result of resolving
 * one. A static template could not express that without a placeholder language.
 *
 * `tail` is the panel's own hash route after the repository, e.g. "mrs" or "mr/3". It is built by the
 * caller that knows which view its result belongs to, so a listing opens the list and a single
 * request opens that request.
 *
 * Returns nil when there is no repository to point at, so a call that failed early emits no button
 * rather than a link to a panel that would open on nothing.
 */
func mirrorView(owner, repo, tail, title string) map[string]any {
	owner = strings.TrimSpace(owner)
	repo = strings.TrimSpace(repo)
	if owner == "" || repo == "" {
		return nil
	}
	// The repository is TWO path segments in the panel's hash, not one escaped token: `owner/name` is
	// how everybody writes it, and escaping the slash would make it an opaque string.
	route := "/repo/" + url.PathEscape(owner) + "/" + url.PathEscape(repo)
	if tail != "" {
		route += "/" + strings.TrimPrefix(tail, "/")
	}
	if title == "" {
		title = owner + "/" + repo
	}
	return map[string]any{
		"ext":   mirrorExtID,
		"route": route,
		"title": title,
	}
}

// mirrorExtID is the extension that serves the mirror panel.
const mirrorExtID = "gitea"
