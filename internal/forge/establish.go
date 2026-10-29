package forge

import (
	"context"
	"fmt"
	"strings"
)

/**
 * Establish creates a mirror of an upstream repository, on demand.
 *
 * THE MISSING STEP. Everything else here assumes the mirror exists: Files reads it, patches commit
 * to it, pushes come out of it. Nothing created it, so the panel could list eighty GitHub
 * repositories and not one of them could be opened.
 *
 * TWO CALLS THAT MUST HAPPEN IN ORDER, and the reason is a platform limit rather than a preference:
 *
 *  1. CREATE it WITH an initial commit. A repository created empty answers 200 and then refuses
 *     every write with an empty 500 — so the second call has nowhere to put anything.
 *  2. PUSH the upstream's branch onto it. `CreateBranchFromUpstream` fetches the upstream into
 *     memory and pushes it here, which is the same path `git-sync-upstream` uses to follow it later.
 *
 * `owner` is the forge account that will hold the mirror. The repository is created under it rather
 * than under the upstream's owner, because the account that owns the token is the one that can write.
 */
func (c *Client) Establish(ctx context.Context, name, upstreamURL, upstreamBranch, upstreamToken string) (branch string, commits string, err error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "", fmt.Errorf("name is required")
	}
	if strings.TrimSpace(upstreamURL) == "" {
		return "", "", fmt.Errorf("upstreamURL is required")
	}
	if upstreamBranch == "" {
		upstreamBranch = "main"
	}

	if _, cerr := c.CreateRepo(ctx, name, false); cerr != nil {
		return "", "", fmt.Errorf("create the repository: %w", cerr)
	}

	// POINT THE CLIENT AT WHAT IT JUST CREATED. It was built with `NewForHost`, which has no owner or
	// repository — every call after this one names a path, and without this they resolve against an
	// empty owner and the forge answers "repository not found". The account is asked for rather than
	// assumed, because the repository was created under whoever the token belongs to.
	who, werr := c.WhoAmI(ctx)
	if werr != nil {
		return "", "", fmt.Errorf("find the account that owns the new repository: %w", werr)
	}
	c.Owner, c.Repo = who, name

	// The content, from the upstream, under the SAME branch name: a mirror whose main is called
	// something else would break every later step that assumes the upstream's names.
	//
	// FORCED, and only here. Creating the repository (above) necessarily writes an initial commit, so
	// its `main` already exists — and the upstream's `main` shares no ancestor with it, which makes the
	// push a non-fast-forward and gets it refused. This is the one call where replacing that branch is
	// the intent rather than a collision: the initial commit exists only because the platform would
	// not let us create an empty repository, and its content (a placeholder README) is not wanted.
	sha, perr := c.ReplaceBranchFromUpstream(ctx, upstreamBranch, upstreamURL, upstreamBranch, upstreamToken)
	if perr != nil {
		return "", "", fmt.Errorf("copy %s from the upstream: %w", upstreamBranch, perr)
	}
	return upstreamBranch, sha, nil
}
