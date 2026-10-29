package forge

import (
	"context"
	"errors"
	"fmt"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/storage/memory"
)

/**
 * PushBranch sends one branch from this forge to another repository, under a chosen name.
 *
 * WHY NOT THE PUSH MIRROR: a mirror pushes EVERY ref, so a failed experiment, a half-finished idea
 * and a misnamed branch all land on the upstream the moment they are committed. This pushes the one
 * branch the caller asked for; nothing else leaves the mirror.
 *
 * The branch name may differ at the destination. That is not decoration: a caller can push every
 * attempt as `agent/<topic>` without inventing a new name each time, or keep the upstream free of
 * the local branch naming.
 *
 * Returns the commit sha that was pushed, so the caller can open a pull request against exactly what
 * arrived rather than re-reading it.
 */
func (c *Client) PushBranch(ctx context.Context, branch, destURL, destBranch, destToken string) (string, error) {
	if branch == "" || destURL == "" {
		return "", fmt.Errorf("branch and destURL are required")
	}
	if destBranch == "" {
		destBranch = branch
	}

	// The commit that will be sent, read from the forge so the returned sha is the one the forge has.
	var local struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	if err := c.do(ctx, "GET", "/repos/"+c.path()+"/branches/"+esc(branch), nil, &local); err != nil {
		return "", err
	}
	if local.Commit.ID == "" {
		return "", fmt.Errorf("branch %q has no head commit", branch)
	}

	// A bare in-memory repo: nothing is checked out, only objects are moved from one remote to
	// another. Nothing touches the disk.
	repo, err := git.Init(memory.NewStorage(), nil)
	if err != nil {
		return "", fmt.Errorf("in-memory repo: %w", err)
	}
	const remoteName = "origin"
	src := git.NewRemote(repo.Storer, &config.RemoteConfig{
		Name: remoteName,
		URLs: []string{c.pushURL()},
	})
	srcRef := "refs/heads/" + branch
	if err := src.FetchContext(ctx, &git.FetchOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("+" + srcRef + ":refs/remotes/" + remoteName + "/" + branch),
		},
		Auth:  authFor(c.pushURL(), c.token),
		Force: true,
	}); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return "", fmt.Errorf("read %s from the mirror: %w", branch, err)
	}

	// Push to the destination.
	//
	// NOT forced: the receiving side refuses a non-fast-forward, which is the check that matters — a
	// caller overwriting an upstream branch somebody else moved is losing work that exists nowhere
	// else. If that is ever wanted it should be a separate, explicitly named operation.
	dest := git.NewRemote(repo.Storer, &config.RemoteConfig{
		Name: remoteName,
		URLs: []string{destURL},
	})
	if err := dest.PushContext(ctx, &git.PushOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("refs/remotes/" + remoteName + "/" + branch + ":refs/heads/" + destBranch),
		},
		Auth: authFor(destURL, destToken),
	}); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		// A rejection because the remote branch moved is worth saying plainly: the caller can then
		// pull the remote's version, look at it, and decide — rather than reading "push failed".
		if strings.Contains(err.Error(), "non-fast-forward") ||
			strings.Contains(err.Error(), "rejected") {
			return "", fmt.Errorf(
				"%s already exists on the destination and has commits this one does not; "+
					"push to a new branch name instead of overwriting it: %w", destBranch, err)
		}
		return "", fmt.Errorf("push %s: %w", destBranch, err)
	}
	return local.Commit.ID, nil
}
