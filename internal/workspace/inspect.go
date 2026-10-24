package workspace

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// Inspecting what changed, and undoing it.

// DiffOptions selects what to compare.
type DiffOptions struct {
	// Path restricts the diff to one file. Empty = everything changed.
	Path string
	// Staged compares HEAD against the index instead of the working tree.
	Staged bool
}

// Diff renders the workspace's changes as a unified diff.
//
// go-git has no Worktree.Diff, so this is computed here: for each changed path, the content at
// HEAD against the content on disk (or in the index). The line-level algorithm lives in diff.go
// and was written for exactly this — a diff that does not need a git binary.
func (w *Workspace) Diff(o DiffOptions) (string, error) {
	r, err := w.open()
	if err != nil {
		return "", err
	}
	head, err := r.Head()
	if err != nil {
		// An unborn HEAD (nothing committed yet) means every file is new.
		head = &plumbing.Reference{}
	}

	var headTree *object.Tree
	if head.Hash().IsZero() == false {
		commit, cerr := r.CommitObject(head.Hash())
		if cerr == nil {
			headTree, _ = commit.Tree()
		}
	}

	paths, err := w.changedPaths(o)
	if err != nil {
		return "", err
	}

	var b []byte
	for _, p := range paths {
		old, existed := treeFile(headTree, p)
		next, rerr := w.ReadFile(p)
		present := rerr == nil
		if !present {
			next = nil
		}
		if existed && present && string(old) == string(next) {
			continue
		}
		b = append(b, unifiedDiff(p, old, next, isBinary(old) || isBinary(next))...)
	}
	return string(b), nil
}

// treeFile reads one path out of a tree, reporting whether it was there.
func treeFile(t *object.Tree, path string) ([]byte, bool) {
	if t == nil {
		return nil, false
	}
	f, err := t.File(path)
	if err != nil {
		return nil, false
	}
	rd, err := f.Reader()
	if err != nil {
		return nil, false
	}
	defer func() { _ = rd.Close() }()
	data, err := io.ReadAll(rd)
	if err != nil {
		return nil, false
	}
	return data, true
}

// changedPaths returns the paths to consider, honoring the Path filter.
func (w *Workspace) changedPaths(o DiffOptions) ([]string, error) {
	if o.Path != "" {
		if _, err := w.safePath(o.Path); err != nil {
			return nil, err
		}
		return []string{o.Path}, nil
	}
	statuses, err := w.Status()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(statuses))
	for _, s := range statuses {
		out = append(out, s.Path)
	}
	sort.Strings(out)
	return out, nil
}

// ResetOptions selects what to discard.
type ResetOptions struct {
	// Path restricts the reset to one file. Empty = everything changed.
	Path string
	// Hard also removes untracked files. Without it they are left alone: a file a session
	// created and has not committed is easy to delete by accident, and hard is explicit.
	Hard bool
}

// Reset discards working-tree changes, restoring the files to HEAD.
//
// This is the local "I did not mean that" — it never touches the remote and never moves HEAD, so
// it cannot lose a commit.
func (w *Workspace) Reset(o ResetOptions) (int, error) {
	r, err := w.open()
	if err != nil {
		return 0, err
	}
	wt, err := r.Worktree()
	if err != nil {
		return 0, err
	}

	var paths []string
	if o.Path != "" {
		if _, perr := w.safePath(o.Path); perr != nil {
			return 0, perr
		}
		paths = []string{o.Path}
	} else {
		statuses, serr := w.Status()
		if serr != nil {
			return 0, serr
		}
		for _, s := range statuses {
			paths = append(paths, s.Path)
		}
	}

	n := 0
	for _, p := range paths {
		if o.Hard {
			// Untracked files are not in HEAD, so restoring them is meaningless — removing is
			// the only thing that makes "discard" true.
			if _, ok := w.tracked(p); !ok {
				if rmErr := w.RemoveFile(p); rmErr == nil {
					n++
				}
				continue
			}
		}
		if err := wt.Restore(&git.RestoreOptions{Files: []string{p}}); err == nil {
			n++
		}
	}
	return n, nil
}

// tracked reports whether a path exists in HEAD.
func (w *Workspace) tracked(path string) (bool, bool) {
	r, err := w.open()
	if err != nil {
		return false, false
	}
	head, err := r.Head()
	if err != nil {
		return false, false
	}
	commit, err := r.CommitObject(head.Hash())
	if err != nil {
		return false, false
	}
	tree, err := commit.Tree()
	if err != nil {
		return false, false
	}
	if _, ferr := tree.File(path); ferr != nil {
		return false, false
	}
	return true, true
}

// BlameLine is one line's origin.
type BlameLine struct {
	Number int
	SHA    string
	Author string
	When   string
	Text   string
}

// Blame reports who last changed each line of a file, within a line range.
//
// Bounded on purpose: a caller asking "who wrote this" is looking at a few dozen lines, and an
// unbounded blame of a large file produces thousands of rows that bury the answer.
func (w *Workspace) Blame(path string, from, to int) ([]BlameLine, error) {
	if _, err := w.safePath(path); err != nil {
		return nil, err
	}
	if from <= 0 {
		from = 1
	}
	if to <= 0 {
		to = from + 199
	}
	if to < from {
		return nil, fmt.Errorf("end line %d is before start line %d", to, from)
	}

	r, err := w.open()
	if err != nil {
		return nil, err
	}
	head, err := r.Head()
	if err != nil {
		return nil, fmt.Errorf("nothing is committed yet, so there is no history to blame")
	}
	commit, err := r.CommitObject(head.Hash())
	if err != nil {
		return nil, err
	}
	res, err := git.Blame(commit, path)
	if err != nil {
		return nil, fmt.Errorf("blame %s: %w", path, err)
	}

	var out []BlameLine
	for i, l := range res.Lines {
		if l == nil {
			continue
		}
		// Blame lines carry no number of their own; position in the result IS the line number.
		lineNo := i + 1
		if lineNo < from || lineNo > to || l.Hash.IsZero() {
			continue
		}
		out = append(out, BlameLine{
			Number: lineNo,
			SHA:    l.Hash.String(),
			Author: l.AuthorName,
			When:   l.Date.Format("2006-01-02"),
			Text:   strings.TrimSuffix(l.Text, "\n"),
		})
	}
	return out, nil
}

// PullOptions selects what to fast-forward to.
type PullOptions struct {
	// Branch is the branch to update the current one from. Empty = the current branch's
	// upstream.
	Branch string
	Token  string
}

// Pull fast-forwards the current branch to its remote.
//
// FAST-FORWARD ONLY, never merge. Local commits that the remote does not have mean the histories
// diverged, and a merge would create a commit the caller did not ask for — on a branch that is
// about to be pushed as a pull request, that is exactly the wrong surprise. Divergence is
// reported so the caller decides.
func (w *Workspace) Pull(o PullOptions) (oldSHA, newSHA string, err error) {
	r, err := w.open()
	if err != nil {
		return "", "", err
	}
	head, err := r.Head()
	if err != nil {
		return "", "", err
	}
	if !head.Name().IsBranch() {
		return "", "", fmt.Errorf("HEAD is detached; check out a branch before pulling")
	}
	branch := o.Branch
	if branch == "" {
		branch = head.Name().Short()
	}

	po := &git.PullOptions{RemoteName: "origin", ReferenceName: plumbing.NewBranchReferenceName(branch)}
	if o.Token != "" {
		po.Auth = basicAuth(o.Token)
	}
	wt, err := r.Worktree()
	if err != nil {
		return "", "", err
	}

	old := head.Hash().String()
	perr := wt.Pull(po)
	if perr != nil {
		if errors.Is(perr, git.NoErrAlreadyUpToDate) {
			return old, old, nil
		}
		// Non-fast-forward is the case worth naming: the caller has local commits and the
		// remote moved, and only they can decide which to keep.
		if errors.Is(perr, git.ErrNonFastForwardUpdate) {
			return old, "", fmt.Errorf(
				"cannot fast-forward: this branch has commits the remote does not, and the remote has moved. " +
					"Nothing was changed. Push your branch as a pull request, or reset it with git-reset")
		}
		return old, "", fmt.Errorf("pull: %w", perr)
	}
	newHead, herr := r.Head()
	if herr != nil {
		return old, "", herr
	}
	return old, newHead.Hash().String(), nil
}
