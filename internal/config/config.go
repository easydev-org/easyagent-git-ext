// Package config holds the extension's configuration knobs.
package config

// Config knob names, as declared in manifest.yaml.
const (
	// KnobToken is the GitHub personal access token.
	//
	// Reads on a public repository work WITHOUT it; every write needs one. Keeping it a single
	// knob rather than a per-repository setting means a session always acts as ONE identity — a
	// call cannot name a different credential and act as somebody else.
	KnobToken = "token"
	// KnobAPIURL overrides the API root, for GitHub Enterprise.
	KnobAPIURL = "api-url"
	// KnobWatchRepos is a comma-separated list of owner/name repositories whose releases are
	// published on the `github` channel.
	//
	// A LIST rather than "watch everything the token can see": polling every repository a token
	// has access to would hit the rate limit on day one for any real organization, and most of
	// those repositories are not interesting to anyone here. Watching is opt-in per repository.
	KnobWatchRepos = "watch-repos"
	// KnobWatchInterval is the poll interval, e.g. "60s". Empty uses the default.
	KnobWatchInterval = "watch-interval"
	// KnobWatchPrereleases includes prereleases in announcements.
	KnobWatchPrereleases = "watch-prereleases"
)
