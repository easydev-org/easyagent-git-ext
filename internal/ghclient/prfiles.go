package ghclient

import (
	"context"

	gogithub "github.com/google/go-github/v93/github"
)

// What a pull request actually changed.
//
// Reviewing a PR without this is reviewing blind: the title and the branch names say what the
// author INTENDED, and only the diff says what they did.

// FileChange is one file in a pull request.
type FileChange struct {
	Path      string
	Status    string // added | modified | removed | renamed
	Additions int
	Deletions int
	// Patch is the per-file diff. GitHub truncates it for very large files, so PatchTruncated
	// says whether this text is the whole change or only part of it.
	Patch string
	// PatchTruncated is true when GitHub omitted the patch entirely (binary files, or a diff
	// larger than its limit). A caller must not read an empty Patch as "no change".
	PatchTruncated bool
	PreviousPath   string // set when Status == "renamed"
}

// PRFiles lists the files a pull request changes. Paged.
//
// A file whose Patch comes back empty is marked Truncated rather than presented as an empty
// diff: "GitHub did not send the patch" and "the patch is empty" are different facts, and the
// second one is almost never true for a changed file.
func (c *Client) PRFiles(ctx context.Context, number int) ([]FileChange, error) {
	var out []FileChange
	err := paginate(func(page int) (*gogithub.Response, error) {
		files, resp, err := c.gh.PullRequests.ListFiles(ctx, c.Owner, c.Repo, number,
			&gogithub.ListOptions{Page: page, PerPage: 100})
		c.note(resp)
		if err != nil {
			return resp, err
		}
		for _, f := range files {
			fc := FileChange{
				Path:      f.GetFilename(),
				Status:    f.GetStatus(),
				Additions: f.GetAdditions(),
				Deletions: f.GetDeletions(),
				Patch:     f.GetPatch(),
			}
			if fc.Status == "renamed" {
				fc.PreviousPath = f.GetPreviousFilename()
			}
			// GitHub omits `patch` for binary files and for diffs over its size limit.
			if fc.Patch == "" && (fc.Additions > 0 || fc.Deletions > 0) {
				fc.PatchTruncated = true
			}
			out = append(out, fc)
		}
		return resp, nil
	})
	return out, err
}

// PRDiff returns the whole pull request as one unified diff.
//
// Uses the raw media type, which is the diff GitHub itself shows — one document, in file order,
// rather than per-file patches concatenated by hand.
func (c *Client) PRDiff(ctx context.Context, number int) (string, error) {
	raw, resp, err := c.gh.PullRequests.GetRaw(ctx, c.Owner, c.Repo, number,
		gogithub.RawOptions{Type: gogithub.Diff})
	c.note(resp)
	if err != nil {
		return "", wrap(err)
	}
	return raw, nil
}

// PRDiffStat summarizes a pull request without the diff body: which files, how much.
func (c *Client) PRDiffStat(ctx context.Context, number int) (files int, additions, deletions int, err error) {
	changes, ferr := c.PRFiles(ctx, number)
	if ferr != nil {
		return 0, 0, 0, ferr
	}
	for _, f := range changes {
		additions += f.Additions
		deletions += f.Deletions
	}
	return len(changes), additions, deletions, nil
}
