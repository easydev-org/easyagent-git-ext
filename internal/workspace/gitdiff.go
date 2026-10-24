package workspace

import (
	"bytes"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/format/index"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// Diff using go-git's own diff machinery.
//
// THE PROBLEM THIS SOLVES: `object.DiffTree` compares two TREES, and the working tree is not
// one — uncommitted edits are bytes on disk that have never been hashed into an object. Getting
// from "files on disk" to "a tree" is the whole difficulty, and it is why an earlier version
// computed the diff by hand.
//
// THE APPROACH: stage everything into the index, build a tree from that index, and let go-git
// diff HEAD's tree against it. Staging is what `git diff` itself does conceptually; the
// difference is that git compares index-to-worktree in memory, while this writes the index.
//
// WHAT IT COSTS: the index is left holding the staged state. In this extension that is harmless
// — there is no `add` tool, `git-ws-commit` stages everything anyway, and `git-ws-reset` restores
// from HEAD rather than from the index. It would only matter for a caller doing selective
// staging, which this toolset does not offer.

// DiffGoGit renders the workspace's changes as a unified diff, computed by go-git.
func (w *Workspace) DiffGoGit(o DiffOptions) (string, error) {
	r, err := w.open()
	if err != nil {
		return "", err
	}
	wt, err := r.Worktree()
	if err != nil {
		return "", err
	}

	// Stage everything so the index describes the working tree. .git is never touched by
	// go-git's Add, and ignored files are skipped per .gitignore.
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		return "", fmt.Errorf("stage for diff: %w", err)
	}

	idx, err := r.Storer.Index()
	if err != nil {
		return "", err
	}
	newTreeHash, err := buildTreeFromIndex(r, idx)
	if err != nil {
		return "", fmt.Errorf("build tree: %w", err)
	}
	newTree, err := r.TreeObject(newTreeHash)
	if err != nil {
		return "", err
	}

	// An unborn HEAD means everything is new: diff against the empty tree.
	var oldTree *object.Tree
	head, herr := r.Head()
	if herr == nil {
		commit, cerr := r.CommitObject(head.Hash())
		if cerr == nil {
			oldTree, _ = commit.Tree()
		}
	}
	if oldTree == nil {
		oldTree = &object.Tree{}
	}

	changes, err := object.DiffTree(oldTree, newTree)
	if err != nil {
		return "", fmt.Errorf("diff trees: %w", err)
	}
	// Restrict to one path when asked.
	if o.Path != "" {
		if _, perr := w.safePath(o.Path); perr != nil {
			return "", perr
		}
		filtered := make(object.Changes, 0, len(changes))
		p := strings.TrimPrefix(path.Clean(o.Path), "./")
		for _, c := range changes {
			from, to := changePaths(c)
			if from == p || to == p {
				filtered = append(filtered, c)
			}
		}
		changes = filtered
	}
	if len(changes) == 0 {
		return "", nil
	}

	patch, err := changes.Patch()
	if err != nil {
		return "", fmt.Errorf("patch: %w", err)
	}
	var buf bytes.Buffer
	if err := patch.Encode(&buf); err != nil {
		return "", fmt.Errorf("encode diff: %w", err)
	}
	return buf.String(), nil
}

// changePaths returns the from/to paths of a change, either of which may be empty.
func changePaths(c *object.Change) (from, to string) {
	if c.From.Name != "" {
		from = c.From.Name
	}
	if c.To.Name != "" {
		to = c.To.Name
	}
	return from, to
}

// buildTreeFromIndex writes the tree objects an index describes, and returns the root hash.
//
// Reimplements go-git's internal buildTreeHelper, which is unexported. Same algorithm: group
// entries by directory, emit a tree per directory bottom-up, and let the storer dedupe.
func buildTreeFromIndex(r *git.Repository, idx *index.Index) (plumbing.Hash, error) {
	trees := map[string]*object.Tree{"": {}}
	seen := map[string]bool{}

	for _, e := range idx.Entries {
		var full string
		for _, part := range strings.Split(e.Name, "/") {
			parent := full
			full = path.Join(full, part)
			if seen[full] {
				continue
			}
			te := object.TreeEntry{Name: path.Base(full)}
			if full == e.Name {
				te.Mode = e.Mode
				te.Hash = e.Hash
			} else {
				te.Mode = filemode.Dir
				trees[full] = &object.Tree{}
			}
			trees[parent].Entries = append(trees[parent].Entries, te)
			seen[full] = true
		}
	}

	return writeTreeRecursive(r, "", trees)
}

func writeTreeRecursive(r *git.Repository, parent string, trees map[string]*object.Tree) (plumbing.Hash, error) {
	t := trees[parent]
	// Git requires tree entries sorted by name, with directories compared as "name/".
	sort.Slice(t.Entries, func(i, j int) bool {
		a, b := t.Entries[i], t.Entries[j]
		if a.Mode == filemode.Dir {
			a.Name += "/"
		}
		if b.Mode == filemode.Dir {
			b.Name += "/"
		}
		return a.Name < b.Name
	})

	for i, e := range t.Entries {
		if e.Mode != filemode.Dir {
			continue
		}
		h, err := writeTreeRecursive(r, path.Join(parent, e.Name), trees)
		if err != nil {
			return plumbing.ZeroHash, err
		}
		t.Entries[i].Hash = h
	}

	obj := r.Storer.NewEncodedObject()
	if err := t.Encode(obj); err != nil {
		return plumbing.ZeroHash, err
	}
	hash := obj.Hash()
	if r.Storer.HasEncodedObject(hash) == nil {
		return hash, nil
	}
	if _, err := r.Storer.SetEncodedObject(obj); err != nil {
		return plumbing.ZeroHash, err
	}
	return hash, nil
}
