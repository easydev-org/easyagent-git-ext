package tools

import (
	"net/url"
	"strconv"
	"strings"
)

// The extension's own UI id. Matches the Service (`gitdev-ext`), the mount (`/ext/gitdev`) and the
// permission namespace; one name, so a mismatch shows up as a 404 rather than as wrong data.
const uiExtID = "gitdev"

// Card templates, named rather than pathed.
//
// A card is a RENDER of the call, not a page: the host hands it the tool's input, structured data and
// output, and the UI picks a renderer by name. That is why a template is a constant and a route is
// not — there is nowhere to navigate to, and nothing to fetch.
//
// It also means a card never fails the way a page can: the workspace it describes may be gone, but
// the card is reading the tool's own result, which is still in the message.
const (
	// The MIRROR's own results. A card is worth having here because the answer is a LIST or a COUNT
	// that a reader scans — a file tree, a diff's file list, what a commit changed — and text alone
	// makes them read a paragraph to find the one line they wanted.
	cardTemplateFiles  = "git-file-list"
	cardTemplateFile   = "git-file-read"
	cardTemplateCommit = "git-file-patch"
	cardTemplateSync   = "git-sync-upstream"

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
		// AN ISSUE gets its own page now. It used to return nil — "there is nowhere to go" — which
		// meant a card for an issue offered no button while a card for a pull request did, for no
		// reason the reader could see. GitHub numbers both in one sequence, so one route serves both
		// and the page says which it is.
		// The repository is TWO path segments — `#/issue/<owner>/<name>/<n>` — matching how the
		// panel's router reads it and how everybody writes a repository. One escaped token would be
		// opaque, and an unescaped slash would make the number unreachable.
		return map[string]any{
			"ext":   uiExtID,
			"route": "/issue/" + repo + "/" + itoa(number),
			"title": title,
		}
	}
	return pullView(repo, number, title)
}

// itoa avoids pulling strconv in for one call in one file.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
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
