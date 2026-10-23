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
)
