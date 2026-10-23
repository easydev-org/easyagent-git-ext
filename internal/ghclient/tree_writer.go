package ghclient

import (
	"context"
	"fmt"

	gogithub "github.com/google/go-github/v93/github"
)

// Committing a set of files onto an existing branch.
//
// WHY THIS EXISTS, and why it does not reuse the easyvcs gitbridge export path:
//
// gitbridge builds each easyvcs revision into a FRESH git commit with no parent. That is right
// for a mirror of stored content, and wrong for a pull request: the branch came out as a root
// commit whose tree happened to match main, and GitHub refused the PR with "the branch has no
// history in common with main" — it compares commit ANCESTRY, not tree contents. The push
// succeeded, so the failure looked like a PR problem rather than a history problem.
//
// GitHub's Git Data API does it properly: create blobs, create a tree based on the parent's
// tree, create a commit naming that parent explicitly, then point a new ref at it. The result
// is an ordinary descendant of the base branch.

// CommitFiles writes files onto a NEW branch based on `base`, and returns the branch name.
//
// The whole commit is built before the branch is created, so a failure part-way through leaves
// no half-made branch behind. The branch is new every time (the caller passes a unique name),
// so this never rewrites history a reviewer is looking at.
func (c *Client) CommitFiles(ctx context.Context, branch, base, message string, files map[string]string, deletes []string) (string, error) {
	if branch == "" || base == "" {
		return "", fmt.Errorf("branch and base are required")
	}
	if len(files) == 0 && len(deletes) == 0 {
		return "", fmt.Errorf("nothing to commit")
	}

	// 1. The base commit, whose tree the new tree is layered on and whose SHA becomes the parent.
	baseRef, resp, err := c.gh.Git.GetRef(ctx, c.Owner, c.Repo, "refs/heads/"+base)
	c.note(resp)
	if err != nil {
		return "", wrap(err)
	}
	baseSHA := baseRef.GetObject().GetSHA()
	baseCommit, resp, err := c.gh.Git.GetCommit(ctx, c.Owner, c.Repo, baseSHA)
	c.note(resp)
	if err != nil {
		return "", wrap(err)
	}
	baseTreeSHA := baseCommit.GetTree().GetSHA()

	// 2. One blob per file. Done before the tree so a failed upload creates nothing visible.
	entries := make([]*gogithub.TreeEntry, 0, len(files)+len(deletes))
	for path, content := range files {
		blob, resp, err := c.gh.Git.CreateBlob(ctx, c.Owner, c.Repo, gogithub.Blob{
			Content:  content,
			Encoding: gogithub.String("utf-8"),
		})
		c.note(resp)
		if err != nil {
			return "", fmt.Errorf("blob %s: %w", path, wrap(err))
		}
		entries = append(entries, &gogithub.TreeEntry{
			Path: gogithub.String(path),
			Mode: gogithub.String("100644"),
			Type: gogithub.String("blob"),
			SHA:  gogithub.String(blob.GetSHA()),
		})
	}
	// A deletion is expressed as a tree entry whose SHA is nil.
	for _, path := range deletes {
		entries = append(entries, &gogithub.TreeEntry{
			Path: gogithub.String(path),
			Mode: gogithub.String("100644"),
			Type: gogithub.String("blob"),
			SHA:  nil,
		})
	}

	// 3. The tree, layered on the base tree so untouched files are inherited.
	tree, resp, err := c.gh.Git.CreateTree(ctx, c.Owner, c.Repo, baseTreeSHA, entries)
	c.note(resp)
	if err != nil {
		return "", fmt.Errorf("tree: %w", wrap(err))
	}

	// 4. The commit, naming the base as its parent — this is the line that makes the branch
	//    share the base branch's history.
	commit, resp, err := c.gh.Git.CreateCommit(ctx, c.Owner, c.Repo, gogithub.Commit{
		Message: gogithub.String(message),
		Tree:    &gogithub.Tree{SHA: tree.SHA},
		Parents: []*gogithub.Commit{{SHA: gogithub.String(baseSHA)}},
	}, nil)
	c.note(resp)
	if err != nil {
		return "", fmt.Errorf("commit: %w", wrap(err))
	}

	// 5. The branch. Created last, so nothing exists unless the commit does.
	if _, resp, err := c.gh.Git.CreateRef(ctx, c.Owner, c.Repo, gogithub.CreateRef{
		Ref: "refs/heads/" + branch,
		SHA: commit.GetSHA(),
	}); err != nil {
		return "", fmt.Errorf("create branch %s: %w", branch, wrap(err))
	} else {
		c.note(resp)
	}
	return branch, nil
}
