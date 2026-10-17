// Package config parses the git-ext config knobs into a strongly-typed server
// list. The config surface is intentionally small:
//
//   - `servers` (secret, JSON): a list of {host, kind, token, user?} — the
//     credential for each git host. It is a SECRET (the agent withholds its
//     value from clients; the UI shows set/not-set only).
//   - `git-protocol` (enum): https | ssh — the transport preference (ssh is
//     not yet implemented; the enum is reserved).
//   - `default-branch` (string): branch used when a remote has no HEAD hint.
//
// A server is selected by the remote URL's host; an unknown host falls back to
// anonymous access (public repos) unless `require-auth` is set.
package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// Knob names — the single source of truth for the manifest + readers.
const (
	KnobServers       = "servers"
	KnobGitProtocol   = "git-protocol"
	KnobDefaultBranch = "default-branch"
	KnobRequireAuth   = "require-auth"
)

// ServerKind is the forge a host belongs to. It selects the HTTPS basic-auth
// username convention (GitHub wants `x-access-token`, GitLab `oauth2`, Gitea a
// real username). `generic` uses the plain username.
type ServerKind string

const (
	KindGitHub  ServerKind = "github"
	KindGitLab  ServerKind = "gitlab"
	KindGitea   ServerKind = "gitea"
	KindGeneric ServerKind = "generic"
)

// Server is one configured git host.
type Server struct {
	// Host is the hostname (no scheme, no path), e.g. "github.com".
	Host string `json:"host"`
	// Kind selects the auth convention. Empty defaults to "generic".
	Kind ServerKind `json:"kind"`
	// Token is the PAT. Empty = anonymous.
	Token string `json:"token"`
	// User overrides the basic-auth username (Gitea needs the real account).
	User string `json:"user"`
}

// Config is the parsed, validated git-ext configuration.
type Config struct {
	Servers       []Server
	GitProtocol   string // "https" (default) | "ssh"
	DefaultBranch string // "main" (default)
	RequireAuth   bool
}

// Parse builds a Config from the raw knob values (as read from extension
// config). A malformed `servers` value yields an empty list (the tools then
// report a clear "not configured" error rather than panicking).
func Parse(get func(name string) string) (Config, error) {
	servers, err := ParseServers(get(KnobServers))
	if err != nil {
		return Config{}, err
	}
	c := Config{
		Servers:       servers,
		GitProtocol:   orDefault(get(KnobGitProtocol), "https"),
		DefaultBranch: orDefault(get(KnobDefaultBranch), "main"),
		RequireAuth:   get(KnobRequireAuth) == "true",
	}
	switch c.GitProtocol {
	case "https", "ssh":
	default:
		return Config{}, fmt.Errorf("config: git-protocol %q: want https or ssh", c.GitProtocol)
	}
	return c, nil
}

// ParseServers parses the `servers` JSON list, dropping invalid entries (a bad
// row must never take down the whole extension). Entries without a host are
// dropped; an empty token is allowed (anonymous / public repos).
func ParseServers(raw string) ([]Server, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var rows []Server
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		return nil, fmt.Errorf("config: servers is not a JSON list: %w", err)
	}
	out := make([]Server, 0, len(rows))
	seen := map[string]bool{}
	for _, s := range rows {
		host := normalizeHost(s.Host)
		if host == "" || seen[host] {
			continue
		}
		seen[host] = true
		if s.Kind == "" {
			s.Kind = KindGeneric
		}
		s.Host = host
		s.Token = strings.TrimSpace(s.Token)
		s.User = strings.TrimSpace(s.User)
		out = append(out, s)
	}
	return out, nil
}

// normalizeHost extracts a bare hostname from a host string that may carry a
// scheme or path ("https://github.com/x" -> "github.com").
func normalizeHost(h string) string {
	h = strings.TrimSpace(strings.ToLower(h))
	if h == "" {
		return ""
	}
	if strings.Contains(h, "://") {
		if u, err := url.Parse(h); err == nil && u.Host != "" {
			h = u.Host
		}
	}
	if i := strings.IndexByte(h, '/'); i >= 0 {
		h = h[:i]
	}
	// Strip a :port (host matching is by name, not port).
	if i := strings.LastIndexByte(h, ':'); i >= 0 {
		// Keep IPv6 literals intact ([::1]).
		if !strings.Contains(h[i:], "]") {
			h = h[:i]
		}
	}
	return strings.TrimSuffix(h, ".")
}

// ServerForHost returns the configured server for a hostname, or (zero, false)
// when none matches.
func (c Config) ServerForHost(host string) (Server, bool) {
	host = normalizeHost(host)
	for _, s := range c.Servers {
		if s.Host == host {
			return s, true
		}
	}
	return Server{}, false
}

// HostOf extracts the hostname from a git remote URL (https, ssh://, or
// scp-style git@host:path). Returns "" when it cannot be determined.
func HostOf(remote string) string {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return ""
	}
	if strings.Contains(remote, "://") {
		if u, err := url.Parse(remote); err == nil {
			return normalizeHost(u.Host)
		}
		return ""
	}
	// scp-style: [user@]host:path
	if at := strings.IndexByte(remote, '@'); at >= 0 {
		rest := remote[at+1:]
		if colon := strings.IndexByte(rest, ':'); colon > 0 {
			return normalizeHost(rest[:colon])
		}
	}
	return ""
}

// AuthUser is the HTTPS basic-auth username for a server. GitHub accepts
// `x-access-token`, GitLab `oauth2`, Gitea the real account name; a `user`
// override always wins.
func (s Server) AuthUser() string {
	if s.User != "" {
		return s.User
	}
	switch s.Kind {
	case KindGitHub:
		return "x-access-token"
	case KindGitLab:
		return "oauth2"
	default:
		// Gitea and generic hosts want a real username; "token" is the widely
		// accepted placeholder (and is what most hosts ignore).
		return "token"
	}
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return strings.TrimSpace(v)
}
