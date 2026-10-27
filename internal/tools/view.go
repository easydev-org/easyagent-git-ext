package tools

import (
	"net/url"
	"strings"
)

// The extension's own UI id. Matches the Service (`github-ext`), the mount (`/ext/github`) and the
// permission namespace; one name, so a mismatch shows up as a 404 rather than as wrong data.
const uiExtID = "github"

// viewFor builds the `view` entry a tool result carries to point at a UI route.
//
// WHY THE TOOL AND NOT THE MANIFEST: only the tool knows WHERE its results live. `git-ws-clone`
// names the workspace it just created — a value that exists in no argument and in no manifest — and
// `git-status` names the one it was given. A static template cannot express the first case without
// a placeholder language, and a placeholder referencing a result field cannot be validated without
// running the tool.
//
// The cost is that a client cannot know a tool HAS a view until it runs; the benefit is that the
// value is always right and always escaped by the code that understands it.
//
// Returns nil when the workspace id is empty, so a caller that has nothing to point at emits no
// button rather than a link to a broken route.
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
//
// Different from escaping a single segment: a file path is inherently multi-segment, and escaping
// the slashes would turn `src/lib/x.ts` into one segment the router cannot descend into. Each
// segment is escaped separately, which is what a path-shaped route expects.
func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}
