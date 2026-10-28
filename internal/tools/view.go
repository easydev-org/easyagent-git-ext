package tools

import (
	"net/url"
	"strconv"
	"strings"
)

// The extension's own UI id. Matches the Service (`github-ext`), the mount (`/ext/github`) and the
// permission namespace; one name, so a mismatch shows up as a 404 rather than as wrong data.
const uiExtID = "github"

// Card templates, named rather than pathed.
//
// A card is a RENDER of the call, not a page: the host hands it the tool's input, structured data and
// output, and the UI picks a renderer by name. That is why a template is a constant and a route is
// not — there is nowhere to navigate to, and nothing to fetch.
//
// It also means a card never fails the way a page can: the workspace it describes may be gone, but
// the card is reading the tool's own result, which is still in the message.
const (
	cardTemplateClone   = "git-ws-clone"
	cardTemplateRead    = "git-ws-read"
	cardTemplateDiff    = "git-ws-diff"
	cardTemplateStatus  = "git-ws-status"
	cardTemplateLog     = "git-ws-log"
	cardTemplatePRFiles = "gh-pr-files"
	cardTemplatePRDiff  = "gh-pr-diff"
	cardTemplatePRCheck = "gh-pr-checks"
	cardTemplateItem    = "gh-issue-show"
)

// cardFor declares the renderer for a call. No route, no workspace id: the host already holds the
// data this card will draw.
func cardFor(template string) map[string]any {
	if template == "" {
		return nil
	}
	return map[string]any{
		"ext":      uiExtID,
		"template": template,
	}
}

/**
 * viewFor builds the `view` entry: where the reader can GO to browse this call's subject.
 *
 * Kept SEPARATE from `cardFor` because the two fail differently. A card always works — it reads the
 * result. A view is a place: the workspace behind it can expire, and that failure belongs to the
 * moment somebody clicks, not to the moment the message was written. Reporting "expired" inside a
 * card would be reporting it at a time when nothing is wrong.
 *
 * Returns nil when there is nothing to point at, so a caller with no workspace emits no button rather
 * than a link to a broken route.
 */
func viewFor(wsID, route, title string) map[string]any {
	if wsID == "" {
		return nil
	}
	return map[string]any{
		"ext":   uiExtID,
		"route": route,
		"title": title,
	}
}

// pullView points at a pull request's page.
//
// The route is `#/pull/<owner>/<name>/<number>` — the repository as TWO segments, matching how the
// page's own router reads it and how everybody writes it. Escaping the slash would turn it into one
// opaque token.
//
// A pull request has no workspace, so this cannot go through viewFor: the page reads GitHub, which
// is the one place the UI does. It is a view rather than a card for exactly that reason — opening it
// shows the CURRENT state, and a stale one fails visibly instead of quietly.
func pullView(repo string, number int, title string) map[string]any {
	parts := strings.Split(strings.Trim(repo, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || number <= 0 {
		return nil
	}
	return map[string]any{
		"ext":   uiExtID,
		"route": "/pull/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) + "/" + strconv.Itoa(number),
		"title": title,
	}
}

// issueView points at a page for an issue, if one exists.
//
// Only pull requests have one today: an issue needs a page of its own, and a link to a route that
// would render nothing is worse than no link at all.
func issueView(repo string, number int, isPullRequest bool, title string) map[string]any {
	if !isPullRequest {
		return nil
	}
	return pullView(repo, number, title)
}

// wsRoute is the route for one workspace view.
//
// The id is escaped as a path SEGMENT: a workspace id is generated and safe today, but a route built
// by string concatenation is one format change away from producing a link that resolves somewhere
// else. `url.PathEscape` also escapes `/`, which is what a single segment needs.
func wsRoute(wsID, tail string) string {
	return "/ws/" + url.PathEscape(wsID) + tail
}

// escapePath escapes a FILE path for a route, keeping its `/` separators.
func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}
