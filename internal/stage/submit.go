package stage

import (
	"context"
	"fmt"

	"github.com/easylab-platform/easyvcs/gitbridge"
	"github.com/easylab-platform/easyvcs/store"
)

// SubmitParams describes where the staged change should land.
type SubmitParams struct {
	// Dest is the git remote to push to (https://host/owner/repo.git).
	Dest string
	// Branch is the branch to create for the change.
	Branch string
	// Token authenticates the push; the same credential the forge API uses.
	Token string
	// Description becomes the commit message and, by default, the PR title.
	Description string
	Author      store.Author
}

// SubmitResult is what the caller reports back to the agent.
type SubmitResult struct {
	Revision string       `json:"revision"`
	Branch   string       `json:"branch"`
	SHA      string       `json:"sha,omitempty"`
	Files    []FileStatus `json:"files"`
}

// Submit commits the stage and pushes that revision to a branch on the forge.
//
// This is the step that replaces "clone into a sandbox, edit, commit, push". The
// revision is built from the change set in the database, gitbridge turns it into a git
// commit in a temporary directory, and go-git pushes it — no checkout, no git binary,
// no container.
//
// The stage is cleared only AFTER a successful push: a failed push must not destroy the
// edits the user just made, because the retry often needs no changes at all.
func (w *Workspace) Submit(ctx context.Context, p SubmitParams) (SubmitResult, error) {
	if !w.Dirty() {
		return SubmitResult{}, fmt.Errorf("nothing staged")
	}
	files, err := w.Status()
	if err != nil {
		return SubmitResult{}, err
	}

	w.mu.Lock()
	ws := w.ws
	repo := w.repo
	base := w.base
	w.mu.Unlock()

	snap, rev, err := w.Commit(CommitParams{Description: p.Description, Author: p.Author})
	if err != nil {
		return SubmitResult{}, fmt.Errorf("commit: %w", err)
	}
	_ = snap

	// NOT squashed, and the branch starts from the base revision.
	//
	// Squashing exported ONLY the new revision, which made it a ROOT commit with no parent —
	// and GitHub refuses a pull request whose branch "has no history in common with main".
	// The push succeeded and the PR failed, which is the worst shape of failure: a branch
	// nobody asked for, and no review.
	//
	// So the chain is exported from the base these edits were made against, through to the new
	// commit. The branch then shares main's history and the PR is an ordinary one.
	chain, err := w.chainFrom(base, rev.ID)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("revision chain: %w", err)
	}
	shas, err := gitbridge.ExportRevisions(ws, repo, gitbridge.PushOptions{
		Dest:      p.Dest,
		Branch:    p.Branch,
		Revisions: chain,
		Author:    p.Author,
		Token:     p.Token,
	})
	if err != nil {
		return SubmitResult{}, fmt.Errorf("push %s: %w", p.Branch, err)
	}

	res := SubmitResult{Revision: rev.ID, Branch: p.Branch, Files: files}
	if len(shas) > 0 {
		res.SHA = shas[0]
	}

	// Only now is the stage safe to clear.
	w.ResetEdit("")
	return res, nil
}
