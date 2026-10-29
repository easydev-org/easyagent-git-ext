package watch

import (
	"context"

	"github.com/easydev-org/easyagent-gitdev-ext/internal/ghclient"
)

// GHReader adapts the GitHub client to the watcher's ReleaseReader.
//
// The adapter exists so the watch package does not import ghclient: the watcher's job is
// "detect something new and publish it", and it should be testable with a fake that returns two
// releases, not with an HTTP client.
type GHReader struct {
	Client *ghclient.Client
}

// Releases lists releases, mapped to the watcher's own shape.
func (r GHReader) Releases(ctx context.Context, limit int) ([]Release, error) {
	rels, err := r.Client.Releases(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Release, 0, len(rels))
	for _, rel := range rels {
		out = append(out, Release{
			Tag:         rel.TagName,
			Name:        rel.Name,
			Draft:       rel.Draft,
			Prerelease:  rel.Prerelease,
			URL:         rel.HTMLURL,
			PublishedAt: rel.PublishedAt,
		})
	}
	return out, nil
}
