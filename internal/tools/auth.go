package tools

import (
	"github.com/easydev-org/easyagent-gitea-ext/internal/config"
)

// auth is the resolved HTTPS credential for a remote host.
type auth struct {
	user  string
	token string
}

// authFor resolves the credential for a remote URL from the `servers` config:
// the server whose host matches. An unconfigured host is anonymous unless
// require-auth is set.
func (t *toolset) authFor(session, tenant, remote string) auth {
	cfg := t.cfg(session, tenant)
	host := config.HostOf(remote)
	srv, ok := cfg.ServerForHost(host)
	if !ok || srv.Token == "" {
		return auth{}
	}
	// The username must be a placeholder Gitea RECOGNISES ("x-access-token"), never
	// the token itself: the token only ever travels on stdin, so putting it in the
	// command line would leak it into `ps` and the job log.
	return auth{user: srv.AuthUser(), token: srv.Token}
}
