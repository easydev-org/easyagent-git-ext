package tools

import (
	"strings"
)

/**
 * mirrorView names the MIRROR repository a result was about.
 *
 * IT NO LONGER POINTS AT A PANEL. It used to name a second extension whose page browsed the mirror's
 * files; the two extensions are now one, and this one has no file-browsing page — its page shows
 * pull requests. Emitting a route nothing serves would put a button on a card that opens on nothing,
 * which is worse than no button: the reader spends a click discovering that.
 *
 * So the repository is reported as DATA and the caller decides what to do with it. When a mirror page
 * exists, pointing at it is a one-line change here rather than a change at every call site.
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
	if title == "" {
		title = owner + "/" + repo
	}
	out := map[string]any{
		"repo":  owner + "/" + repo,
		"title": title,
	}
	if tail != "" {
		out["at"] = strings.TrimPrefix(tail, "/")
	}
	return out
}
