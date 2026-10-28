package tools

import (
	"net/url"
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
