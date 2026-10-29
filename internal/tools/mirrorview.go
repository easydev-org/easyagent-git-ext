package tools

import (
	"net/url"
	"strings"
)

/**
 * mirrorView points at the MIRROR's file-browsing page for one repository.
 *
 * The route is `#/files/<owner>/<name>?branch=<b>&dir=<d>`, which the panel parses. The BRANCH and
 * DIRECTORY ride in the query rather than the path: a branch name contains slashes (`feat/x`), and
 * putting one in a path segment would change what the route means.
 *
 * `tail` is the panel's own route after the repository — "files/main", "files/main/src", or
 * "files/feat%2Fx/src" for a branch containing a slash.
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
	// The repository is TWO path segments, which is how it is written and how the panel's router
	// reads it; escaping the slash would produce an opaque token.
	route := "/files/" + url.PathEscape(owner) + "/" + url.PathEscape(repo)
	if tail != "" {
		// `tail` is "files/<branch>" or "files/<branch>/<dir>", and a BRANCH NAME MAY CONTAIN
		// SLASHES — `feat/x` is an ordinary name. Splitting on the first slash would read that as
		// branch "feat" and directory "x", which is a different place. So the CALLER passes the
		// two separately, already encoded, using "?" to mark where the directory begins:
		//
		//	files/feat%2Fx          branch feat/x, root
		//	files/main?src/lib      branch main, directory src/lib
		//
		// Without the "?", a tail with no slash is a branch and the root — the common case.
		rest, ok := strings.CutPrefix(strings.TrimPrefix(tail, "/"), "files/")
		if !ok {
			route += "/" + strings.TrimPrefix(tail, "/")
		} else {
			branch, dir, _ := strings.Cut(rest, "?")
			route += "?branch=" + branch // already escaped by the caller
			if dir != "" {
				route += "&dir=" + url.QueryEscape(dir)
			}
		}
	}
	if title == "" {
		title = owner + "/" + repo
	}
	return map[string]any{
		"ext":   uiExtID,
		"route": route,
		"title": title,
	}
}

/**
 * atBranch builds the `tail` mirrorView expects for a branch and an optional path.
 *
 * Exists so no call site has to remember the encoding rule: the BRANCH is escaped as one value
 * (because `feat/x` is an ordinary branch name) and the PATH follows a "?" (because it genuinely is
 * a path). Writing "files/" + branch + "/" + path at each site is what produced a route that
 * silently opened the wrong place.
 */
func atBranch(branch, path string) string {
	t := "files/" + url.QueryEscape(branch)
	if path != "" {
		t += "?" + strings.TrimPrefix(path, "/")
	}
	return t
}
