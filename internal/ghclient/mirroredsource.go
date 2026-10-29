package ghclient

import (
	"context"
	"strings"

	"github.com/easydev-org/easyagent-gitdev-ext/internal/forge"
	"github.com/easydev-org/easyagent-gitdev-ext/internal/forgeconfig"
)

/**
 * MirroredSource answers which repositories the MIRROR has.
 *
 * IT LIVES HERE BECAUSE THE PANEL'S OTHER SOURCES DO, not because it has anything to do with GitHub.
 * The adapter needs the mirror's configuration — which lives alongside the GitHub token in the same
 * extension — and this is where a `TokenFor`-shaped dependency is already the pattern.
 *
 * ONE FORGE CALL, THEN A SET MEMBERSHIP TEST: the alternative is one request per repository, which
 * on a page listing eighty would be eighty round trips to answer a question the forge can answer
 * once.
 */
type MirroredSource struct {
	// Cfg returns the mirror's server list for a tenant.
	Cfg func(tenant string) forgeconfig.Config
	// TenantFrom resolves the caller for a request.
	TenantFrom func(ctx context.Context) (string, error)
}

// Mirrored reports which of `repos` exist in the mirror.
func (s *MirroredSource) Mirrored(ctx context.Context, repos []string) (map[string]bool, error) {
	tenant := ""
	if s.TenantFrom != nil {
		t, err := s.TenantFrom(ctx)
		if err != nil {
			return nil, err
		}
		tenant = t
	}
	cfg := s.Cfg(tenant)
	if len(cfg.Servers) == 0 {
		// No mirror configured: nothing is there, and saying so is the honest answer rather than an
		// error that would hide every row.
		return map[string]bool{}, nil
	}
	srv := cfg.Servers[0]
	// NewForHost, not New: this call is ABOVE a repository — it lists them — and a placeholder
	// remote would put a repository name that does not exist into every error it produced.
	c, err := forge.NewForHost(srv.APIBase(), srv.Token)
	if err != nil {
		return nil, err
	}
	have, err := c.ListRepos(ctx)
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(have))
	for _, n := range have {
		set[strings.ToLower(strings.Trim(n, "/"))] = true
	}
	out := make(map[string]bool, len(repos))
	for _, r := range repos {
		out[r] = set[strings.ToLower(strings.Trim(r, "/"))]
	}
	return out, nil
}
