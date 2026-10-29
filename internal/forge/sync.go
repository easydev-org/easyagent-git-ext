package forge

import (
	"context"
	"errors"
	"fmt"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"
)

/**
 * SyncUpstream fast-forwards a local branch to the upstream's, and reports whether it moved.
 *
 * WHY THIS EXISTS AT ALL: the local forge is a MIRROR of a repository that people also change
 * directly — a pull request merged on the upstream moves main there, and the mirror has no way to
 * hear about it. Without this, a working branch would be cut from a stale main, and the resulting
 * pull request would carry other people's changes as if the author had made them.
 *
 * WHY NOT `mirror-sync`: that endpoint only exists for a repository configured as a PULL MIRROR, and
 * a pull mirror is READ-ONLY — the whole point of this forge is that it accepts commits. The two are
 * mutually exclusive on this platform, so the sync is done here instead.
 *
 * ONLY A FAST-FORWARD: if the upstream's head is not a descendant of the local one, this refuses.
 * That means somebody committed to the local branch directly — a state this tool never creates, and
 * one that must be looked at rather than silently overwritten.
 */
func (c *Client) SyncUpstream(ctx context.Context, branch, upstreamURL, upstreamToken string) (moved bool, localSHA, upstreamSHA string, err error) {
	if branch == "" || upstreamURL == "" {
		return false, "", "", fmt.Errorf("branch and upstreamURL are required")
	}

	// A bare in-memory repository: the sync only moves refs, so no worktree is needed and nothing
	// touches the disk.
	repo, err := git.Init(memory.NewStorage(), nil)
	if err != nil {
		return false, "", "", fmt.Errorf("in-memory repo: %w", err)
	}

	// The LOCAL head, read through the API — this is the fact the decision rests on.
	var local struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	if err := c.do(ctx, "GET", "/repos/"+c.path()+"/branches/"+esc(branch), nil, &local); err != nil {
		return false, "", "", err
	}
	localSHA = local.Commit.ID
	if localSHA == "" {
		return false, "", "", fmt.Errorf("branch %q has no head commit", branch)
	}

	// Fetch the upstream's branch into the in-memory repo.
	//
	// NOT `Depth: 1`. A shallow fetch brings the tip commit but not every object the push needs to
	// send, and the receiving side answers "object not found" for the first one it is missing — the
	// commit's own tree and blobs, or the history the receiver wants to verify the fast-forward
	// against. A full fetch costs more bytes but is the only version that can be pushed onwards.
	// ONE remote name for both directions. go-git matches a refspec's source ref to the remote it
	// belongs to, and a remote-tracking ref written under one name cannot be pushed through another
	// — it answers "remote names don't match". Fetching into `origin/<branch>` and pushing FROM that
	// same ref keeps the name consistent; the URL changes between the two calls, which is what a
	// mirror does anyway.
	const remoteName = "origin"
	remote := git.NewRemote(repo.Storer, &config.RemoteConfig{
		Name: remoteName,
		URLs: []string{upstreamURL},
	})
	refs, err := remote.ListContext(ctx, &git.ListOptions{Auth: authFor(upstreamURL, upstreamToken)})
	if err != nil {
		return false, localSHA, "", fmt.Errorf("list upstream: %w", err)
	}
	want := "refs/heads/" + branch
	for _, r := range refs {
		if r.Name().String() == want {
			upstreamSHA = r.Hash().String()
		}
	}
	if upstreamSHA == "" {
		// The upstream has no such branch. Nothing to follow, and not an error: a new local branch
		// has no counterpart yet, which is the normal case for a working branch.
		return false, localSHA, "", nil
	}
	if upstreamSHA == localSHA {
		return false, localSHA, upstreamSHA, nil
	}

	// The upstream moved. Fetch it.
	if err := remote.FetchContext(ctx, &git.FetchOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("+" + want + ":refs/remotes/" + remoteName + "/" + branch),
		},
		Auth:  authFor(upstreamURL, upstreamToken),
		Force: true,
	}); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return false, localSHA, upstreamSHA, fmt.Errorf("fetch upstream: %w", err)
	}

	// REFUSE BEFORE PUSHING unless the upstream is a descendant of the local head.
	//
	// The forge would refuse a non-fast-forward anyway, but its refusal reads "non-fast-forward
	// update" and names no commits — a reader cannot tell whether the local branch was tampered with
	// or somebody force-pushed upstream. A pre-check can say which, and that is the difference
	// between "this needs a look" and "try again".
	//
	// It normally succeeds: this tool only syncs PROTECTED branches, which are written by merges on
	// the upstream and by nothing else. The check exists for the case that breaks that assumption —
	// a direct push, a misconfigured protection rule — where silently overwriting would lose work.
	if err := assertFastForward(repo, remoteName, branch, localSHA); err != nil {
		return false, localSHA, upstreamSHA, err
	}

	// Push the fetched commit to the local forge as the branch's new head.
	//
	// A NON-FORCE push: the forge rejects it unless the update is a fast-forward, which is exactly
	// the check that must not be re-implemented here — the server knows the local history, this
	// process does not.
	pushRemote := git.NewRemote(repo.Storer, &config.RemoteConfig{
		Name: remoteName,
		URLs: []string{c.pushURL()},
	})
	if err := pushRemote.PushContext(ctx, &git.PushOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("refs/remotes/" + remoteName + "/" + branch + ":" + want),
		},
		Auth: authFor(c.pushURL(), c.token),
	}); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return false, localSHA, upstreamSHA, fmt.Errorf("fast-forward %s: %w", branch, err)
	}
	return true, localSHA, upstreamSHA, nil
}

/**
 * assertFastForward checks that the fetched upstream tip has the local head in its ancestry.
 *
 * A local head that is NOT an ancestor means the branch holds commits the upstream does not have.
 * Syncing would discard them, so this refuses with both shas named — the caller can then look at
 * what is there instead of discovering later that it vanished.
 */
func assertFastForward(repo *git.Repository, remoteName, branch, localSHA string) error {
	upstreamRef, err := repo.Reference(plumbing.NewRemoteReferenceName(remoteName, branch), true)
	if err != nil {
		return fmt.Errorf("read fetched %s: %w", branch, err)
	}
	upstreamHash := upstreamRef.Hash()

	// Walk the upstream's ancestry looking for the local head. The fetched history bounds this, and a
	// protected branch is not deep enough for it to be costly.
	iter, err := repo.Log(&git.LogOptions{From: upstreamHash})
	if err != nil {
		return fmt.Errorf("walk %s: %w", branch, err)
	}
	defer iter.Close()

	for {
		c, cerr := iter.Next()
		if cerr != nil {
			break
		}
		if c.Hash.String() == localSHA {
			return nil
		}
	}
	return fmt.Errorf(
		"refusing to sync %s: the local branch has commits the upstream does not "+
			"(local %s, upstream %s). Fast-forward only — resolve it by hand",
		branch, shortSHA(localSHA), shortSHA(upstreamHash.String()))
}

/**
 * CreateBranchFromUpstream makes a local branch whose content comes from an ARBITRARY branch on the
 * upstream — including one this mirror has never seen.
 *
 * WHY THIS EXISTS: a retry needs to start from somewhere other than main. A caller whose pull request
 * needs rework may want the new attempt based on the branch that is already open, and a branch pushed
 * by somebody else exists only upstream. Branching from main would silently discard that base.
 *
 * HOW: the local forge cannot point a ref at a commit it does not have, so the upstream's history is
 * fetched into an in-memory repository and PUSHED to the local forge under the new name. That is the
 * same path `SyncUpstream` uses to fast-forward, applied to a name of our choosing.
 *
 * Returns the commit the new branch points at.
 */
func (c *Client) CreateBranchFromUpstream(ctx context.Context, newBranch, upstreamURL, srcBranch, upstreamToken string) (string, error) {
	return c.branchFromUpstream(ctx, newBranch, upstreamURL, srcBranch, upstreamToken, false)
}

/**
 * ReplaceBranchFromUpstream does what CreateBranchFromUpstream does, but OVERWRITES an existing
 * branch.
 *
 * THE ONE LEGITIMATE CALLER is establishing a mirror. A repository cannot be created empty on this
 * forge — the create call writes an initial commit or refuses every later write — so its default
 * branch exists before the upstream arrives, shares no ancestor with it, and a normal push is refused
 * as non-fast-forward. Replacing is the intent there; everywhere else an existing name is a
 * collision the caller must see.
 */
func (c *Client) ReplaceBranchFromUpstream(ctx context.Context, newBranch, upstreamURL, srcBranch, upstreamToken string) (string, error) {
	return c.branchFromUpstream(ctx, newBranch, upstreamURL, srcBranch, upstreamToken, true)
}

func (c *Client) branchFromUpstream(ctx context.Context, newBranch, upstreamURL, srcBranch, upstreamToken string, force bool) (string, error) {
	if newBranch == "" || upstreamURL == "" || srcBranch == "" {
		return "", fmt.Errorf("newBranch, upstreamURL and srcBranch are required")
	}
	// THE SAME NAME IS ALLOWED, and it is the point: that is how a repository is seeded from its
	// upstream. The push below is not forced, so an EXISTING local branch is still refused — what
	// this permits is creating a name that is not there yet, which on a fresh repository is `main`.

	repo, err := git.Init(memory.NewStorage(), nil)
	if err != nil {
		return "", fmt.Errorf("in-memory repo: %w", err)
	}
	const remoteName = "origin"
	srcRef := "refs/heads/" + srcBranch

	up := git.NewRemote(repo.Storer, &config.RemoteConfig{
		Name: remoteName,
		URLs: []string{upstreamURL},
	})
	refs, err := up.ListContext(ctx, &git.ListOptions{Auth: authFor(upstreamURL, upstreamToken)})
	if err != nil {
		return "", fmt.Errorf("list upstream: %w", err)
	}
	want := ""
	for _, r := range refs {
		if r.Name().String() == srcRef {
			want = r.Hash().String()
		}
	}
	if want == "" {
		// Named rather than generic: the caller passed a branch, and "that branch is not there" is
		// the fact they need to act on.
		return "", fmt.Errorf("the upstream has no branch %q", srcBranch)
	}

	if err := up.FetchContext(ctx, &git.FetchOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("+" + srcRef + ":refs/remotes/" + remoteName + "/" + srcBranch),
		},
		Auth:  authFor(upstreamURL, upstreamToken),
		Force: true,
	}); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return "", fmt.Errorf("fetch %s: %w", srcBranch, err)
	}

	// Push it to the local forge under the NEW name. Not forced: a branch that already exists there
	// is a name collision the caller must see, not something to overwrite.
	local := git.NewRemote(repo.Storer, &config.RemoteConfig{
		Name: remoteName,
		URLs: []string{c.pushURL()},
	})
	if err := local.PushContext(ctx, &git.PushOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("refs/remotes/" + remoteName + "/" + srcBranch + ":refs/heads/" + newBranch),
		},
		Auth:  authFor(c.pushURL(), c.token),
		Force: force,
	}); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		if strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "rejected") {
			return "", ErrBranchExists
		}
		return "", fmt.Errorf("create %s: %w", newBranch, err)
	}
	return want, nil
}

// shortSHA is the first 12 characters, for an error message that stays readable.
func shortSHA(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

// pushURL is the local forge's clone URL for this repository.
func (c *Client) pushURL() string {
	base := strings.TrimSuffix(c.apiBase, "/api/v1")
	return base + "/" + c.Owner + "/" + c.Repo + ".git"
}

/**
 * authFor builds the credential for one operation, or nil when there is no token.
 *
 * A CREDENTIAL, not a token in the URL. Putting it in the URL only works over https, and the local
 * forge in this deployment is plain http inside the cluster — so the credential has to travel the
 * way go-git's transports expect, which is an `AuthMethod`. It also keeps the token out of every
 * message that might quote the remote's address.
 *
 * The username is arbitrary for both GitHub and Gitea: they authenticate a personal access token by
 * its value, and only require that a username be present.
 */
func authFor(raw, token string) transport.AuthMethod {
	if strings.TrimSpace(token) == "" {
		return nil
	}
	return &http.BasicAuth{Username: "x-access-token", Password: token}
}

var _ = plumbing.Hash{}
