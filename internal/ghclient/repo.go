package ghclient

import (
	"context"
	"fmt"

	gogithub "github.com/google/go-github/v93/github"
)

// Read operations. All of these work anonymously on a public repository — that is the point of
// this extension, so none of them require a token.

// Repo is the metadata a caller usually needs before reading anything.
type Repo struct {
	FullName      string
	DefaultBranch string
	Private       bool
	Description   string
	Stars         int
	Language      string
	License       string
	Archived      bool
}

// Info reads repository metadata.
func (c *Client) Info(ctx context.Context) (Repo, error) {
	r, resp, err := c.gh.Repositories.Get(ctx, c.Owner, c.Repo)
	c.note(resp)
	if err != nil {
		return Repo{}, wrap(err)
	}
	return Repo{
		FullName:      r.GetFullName(),
		DefaultBranch: r.GetDefaultBranch(),
		Private:       r.GetPrivate(),
		Description:   r.GetDescription(),
		Stars:         r.GetStargazersCount(),
		Language:      r.GetLanguage(),
		License:       r.GetLicense().GetSPDXID(),
		Archived:      r.GetArchived(),
	}, nil
}

// TreeEntry is one path in the repository.
type TreeEntry struct {
	Path string
	Type string // "blob" or "tree"
	Size int
}

// Tree lists every path at a ref.
//
// Recursive in ONE request: GitHub returns the whole tree when asked, so listing a repository
// costs one call rather than one per directory. A tree too large for that is REPORTED rather
// than served partially — a caller that took a partial list for the whole repository would skip
// files without knowing.
func (c *Client) Tree(ctx context.Context, ref string) ([]TreeEntry, error) {
	if ref == "" {
		info, err := c.Info(ctx)
		if err != nil {
			return nil, err
		}
		ref = info.DefaultBranch
	}
	tree, resp, err := c.gh.Git.GetTree(ctx, c.Owner, c.Repo, ref, true)
	c.note(resp)
	if err != nil {
		return nil, wrap(err)
	}
	if tree.GetTruncated() {
		return nil, fmt.Errorf("tree for %s is truncated by GitHub (too many entries); read specific paths instead", c.Slug())
	}
	out := make([]TreeEntry, 0, len(tree.Entries))
	for _, e := range tree.Entries {
		out = append(out, TreeEntry{Path: e.GetPath(), Type: e.GetType(), Size: e.GetSize()})
	}
	return out, nil
}

// File is one file's content plus where it came from.
type File struct {
	Path    string
	SHA     string
	Content string
	Size    int
}

// ReadFile reads one file at a ref (empty = the default branch).
//
// Directories and symlinks come back from the same endpoint with no content; both are refused
// explicitly rather than returned as an empty string that a caller would take for an empty
// file.
func (c *Client) ReadFile(ctx context.Context, path, ref string) (File, error) {
	opts := &gogithub.RepositoryContentGetOptions{Ref: ref}
	fileContent, dirContent, resp, err := c.gh.Repositories.GetContents(ctx, c.Owner, c.Repo, path, opts)
	c.note(resp)
	if err != nil {
		return File{}, wrap(err)
	}
	if dirContent != nil {
		return File{}, fmt.Errorf("%s is a directory, not a file", path)
	}
	if fileContent == nil {
		return File{}, fmt.Errorf("%s is not a file", path)
	}
	if fileContent.GetType() == "symlink" {
		return File{}, fmt.Errorf("%s is a symlink to %s; read the target instead",
			path, fileContent.GetTarget())
	}
	content, derr := fileContent.GetContent()
	if derr != nil {
		// Large files come back with encoding "none" and no inline content: saying so beats
		// returning an empty string.
		return File{}, fmt.Errorf("cannot decode %s (%s): %w", path, fileContent.GetEncoding(), derr)
	}
	return File{
		Path: fileContent.GetPath(), SHA: fileContent.GetSHA(),
		Content: content, Size: fileContent.GetSize(),
	}, nil
}
