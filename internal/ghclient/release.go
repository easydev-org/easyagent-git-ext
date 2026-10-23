package ghclient

import (
	"context"
	"encoding/json"

	gogithub "github.com/google/go-github/v93/github"
)

// Releases and packages: what a project PUBLISHES, as opposed to what it contains.

// Release is one published release.
type Release struct {
	TagName     string
	Name        string
	Draft       bool
	Prerelease  bool
	PublishedAt string
	HTMLURL     string
	Body        string
	Assets      []Asset
}

// Asset is one downloadable file on a release.
type Asset struct {
	Name string
	Size int
	URL  string
}

// Releases lists releases, newest first. Paged.
//
// DRAFTS ARE EXCLUDED by GitHub unless the token has push access — so an anonymous reader sees
// fewer releases than a maintainer does for the same repository. That asymmetry is worth
// knowing before treating a count difference as a bug.
func (c *Client) Releases(ctx context.Context, limit int) ([]Release, error) {
	if limit <= 0 {
		limit = 30
	}
	var out []Release
	err := paginate(func(page int) (*gogithub.Response, error) {
		rels, resp, err := c.gh.Repositories.ListReleases(ctx, c.Owner, c.Repo,
			&gogithub.ListOptions{Page: page, PerPage: 100})
		c.note(resp)
		if err != nil {
			return resp, err
		}
		for _, r := range rels {
			if len(out) >= limit {
				break
			}
			out = append(out, releaseFromG(r))
		}
		return resp, nil
	})
	if err != nil {
		return nil, err
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func releaseFromG(r *gogithub.RepositoryRelease) Release {
	assets := make([]Asset, 0, len(r.Assets))
	for _, a := range r.Assets {
		assets = append(assets, Asset{Name: a.GetName(), Size: a.GetSize(), URL: a.GetBrowserDownloadURL()})
	}
	return Release{
		TagName: r.GetTagName(), Name: r.GetName(), Draft: r.GetDraft(),
		Prerelease: r.GetPrerelease(), PublishedAt: r.GetPublishedAt().Format("2006-01-02T15:04:05Z"),
		HTMLURL: r.GetHTMLURL(), Body: r.GetBody(), Assets: assets,
	}
}

// Release reads one release by tag, or the latest when tag is empty.
func (c *Client) Release(ctx context.Context, tag string) (Release, error) {
	var (
		r    *gogithub.RepositoryRelease
		resp *gogithub.Response
		err  error
	)
	if tag == "" {
		r, resp, err = c.gh.Repositories.GetLatestRelease(ctx, c.Owner, c.Repo)
	} else {
		r, resp, err = c.gh.Repositories.GetReleaseByTag(ctx, c.Owner, c.Repo, tag)
	}
	c.note(resp)
	if err != nil {
		return Release{}, wrap(err)
	}
	return releaseFromG(r), nil
}

// Package is one published package.
type Package struct {
	Name       string
	Type       string
	Visibility string
	HTMLURL    string
}

// ListOrgPackages lists an organization's packages. Paged.
//
// `kind` is container | npm | maven | rubygems | nuget | docker.
func (c *Client) ListOrgPackages(ctx context.Context, org, kind string) ([]Package, error) {
	if kind == "" {
		kind = "container"
	}
	var out []Package
	err := paginate(func(page int) (*gogithub.Response, error) {
		pkgs, resp, err := c.gh.Organizations.ListPackages(ctx, org, &gogithub.PackageListOptions{
			PackageType: gogithub.String(kind),
			ListOptions: gogithub.ListOptions{Page: page, PerPage: 100},
		})
		c.note(resp)
		if err != nil {
			return resp, err
		}
		for _, p := range pkgs {
			out = append(out, Package{
				Name: p.GetName(), Type: p.GetPackageType(),
				Visibility: p.GetVisibility(), HTMLURL: p.GetHTMLURL(),
			})
		}
		return resp, nil
	})
	return out, err
}

// PackageVersions lists a package's version tags, newest first. Paged.
func (c *Client) PackageVersions(ctx context.Context, org, kind, name string) ([]string, error) {
	if kind == "" {
		kind = "container"
	}
	var res []string
	err := paginate(func(page int) (*gogithub.Response, error) {
		vers, resp, err := c.gh.Organizations.PackageGetAllVersions(ctx, org,
			kind, name, &gogithub.PackageListOptions{
				ListOptions: gogithub.ListOptions{Page: page, PerPage: 100},
			})
		c.note(resp)
		if err != nil {
			return resp, err
		}
		for _, v := range vers {
			// Metadata is raw JSON (it differs per package type), so the container tags have to
			// be decoded here. Prefer tags: a caller asking "what versions exist" wants the
			// published names, and the internal version id is meaningless to them.
			var meta struct {
				Container struct {
					Tags []string `json:"tags"`
				} `json:"container"`
			}
			_ = json.Unmarshal(v.Metadata, &meta)
			if len(meta.Container.Tags) > 0 {
				res = append(res, meta.Container.Tags...)
			} else if v.GetName() != "" {
				res = append(res, v.GetName())
			}
		}
		return resp, nil
	})
	return res, err
}
