package stage

import (
	"fmt"
	"sort"

	"github.com/easylab-platform/easyvcs/revision"
	"github.com/easylab-platform/easyvcs/store"
)

// FileStatus is one entry in a stage summary.
type FileStatus struct {
	Path      string `json:"path"`
	Status    string `json:"status"` // "added" | "modified" | "deleted"
	Additions int    `json:"additions,omitempty"`
	Deletions int    `json:"deletions,omitempty"`
	Binary    bool   `json:"binary,omitempty"`
}

// Status classifies the pending edits against the base.
//
// "added" and "modified" are distinguished by whether the path exists at the base, which
// is what a reviewer needs: a rename shows up as one add and one delete, and a new file
// reads differently from an edit.
func (w *Workspace) Status() ([]FileStatus, error) {
	w.mu.Lock()
	base, ws, edits := w.base, w.ws, copyEdits(w.edits)
	w.mu.Unlock()

	treeID, err := w.treeOf(ws, base)
	if err != nil {
		return nil, err
	}

	out := make([]FileStatus, 0, len(edits))
	for path, e := range edits {
		old, existed, err := readPath(ws, treeID, path)
		if err != nil {
			return nil, err
		}
		fs := FileStatus{Path: path}
		switch {
		case e.Delete:
			fs.Status = "deleted"
			fs.Deletions = countLines(old)
		case !existed:
			fs.Status = "added"
			fs.Additions = countLines(e.Content)
			fs.Binary = e.Binary
		default:
			fs.Status = "modified"
			if e.Binary || isBinary(old) {
				fs.Binary = true
			} else {
				_, fs.Additions, fs.Deletions = DiffLines(old, e.Content)
			}
		}
		out = append(out, fs)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// Diff renders the pending changes as a unified diff.
//
// Computed here rather than by a git binary: the stage has no checkout, and the whole
// point of this package is to avoid needing one.
func (w *Workspace) Diff() (string, error) {
	w.mu.Lock()
	base, ws, edits := w.base, w.ws, copyEdits(w.edits)
	w.mu.Unlock()

	treeID, err := w.treeOf(ws, base)
	if err != nil {
		return "", err
	}

	paths := make([]string, 0, len(edits))
	for p := range edits {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var b []byte
	for _, p := range paths {
		e := edits[p]
		old, _, err := readPath(ws, treeID, p)
		if err != nil {
			return "", err
		}
		var next []byte
		if !e.Delete {
			next = e.Content
		}
		b = append(b, unifiedDiff(p, old, next, e.Binary)...)
	}
	return string(b), nil
}

// CommitParams describes the revision to create from the stage.
type CommitParams struct {
	Description string
	Author      store.Author
}

// Commit turns the pending edits into a revision on top of the base.
//
// This is the step that makes the stage useful: the revision is an ordinary easyvcs
// snapshot, so gitbridge can push it and easyvcs keeps it as real history. The stage is
// left intact — submitting clears it, committing does not, so a caller can inspect or
// amend before sending anything to the forge.
func (w *Workspace) Commit(p CommitParams) (*store.Snapshot, *store.Revision, error) {
	if !w.Dirty() {
		return nil, nil, fmt.Errorf("nothing staged")
	}
	w.mu.Lock()
	base, ws, edits := w.base, w.ws, copyEdits(w.edits)
	w.mu.Unlock()

	parent, err := ws.GetRevision(base)
	if err != nil {
		return nil, nil, fmt.Errorf("base revision %s: %w", base, err)
	}

	specs := make([]revision.FileChangeSpec, 0, len(edits))
	for path, e := range edits {
		if e.Delete {
			specs = append(specs, revision.FileChangeSpec{Path: path, Delete: true})
			continue
		}
		specs = append(specs, revision.FileChangeSpec{Path: path, Content: e.Content})
	}

	desc := p.Description
	if desc == "" {
		desc = defaultDescription(edits)
	}
	return ws.CommitFromChanges(parent.Hash, specs, desc, p.Author, "")
}

// defaultDescription summarises a change set for a caller that did not write one.
//
// A commit with no message is worse than a terse one: the forge shows it, and "3 files
// changed" at least says what happened.
func defaultDescription(edits map[string]Edit) string {
	var added, modified, deleted int
	for _, e := range edits {
		switch {
		case e.Delete:
			deleted++
		default:
			modified++
		}
	}
	n := len(edits)
	_ = added
	return fmt.Sprintf("stage: %d file(s) changed (%d written, %d deleted)", n, modified, deleted)
}

func countLines(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	n := 1
	for _, c := range b {
		if c == '\n' {
			n++
		}
	}
	return n
}
