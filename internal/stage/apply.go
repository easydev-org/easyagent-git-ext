package stage

import (
	"fmt"
	"strings"
)

// ApplyPatch applies a parsed patch to a staging workspace.
//
// Applied as ONE unit: every operation is validated against the CURRENT workspace content
// before anything is written, so a patch whose third hunk does not apply leaves the stage
// untouched rather than half-edited. A partially applied patch is worse than a rejected one,
// because the caller believes the edit landed and a later review cannot tell which half did.
//
// Paths are confined to the workspace root: an absolute path or one containing ".." is refused,
// since a staging area for a repository has no business writing outside it.
// FileStore is the part of a staging workspace that ApplyPatch needs.
//
// Narrow on purpose: it makes the patch logic testable without an easyvcs store, and it states
// exactly what applying a patch is allowed to touch — read a path, write a path, remove a path.
type FileStore interface {
	Read(path string) ([]byte, bool, error)
	Write(path string, content []byte)
	Remove(path string)
}

func ApplyPatch(w FileStore, text string) ([]PatchOp, error) {
	ops, err := ParsePatch(text)
	if err != nil {
		return nil, err
	}
	if len(ops) == 0 {
		return nil, fmt.Errorf("the patch is empty (no Add/Update/Delete sections)")
	}

	// Resolve every operation against the CURRENT content first, so a validation failure
	// changes nothing.
	type resolved struct {
		op      PatchOp
		content []byte
		existed bool
	}
	plan := make([]resolved, 0, len(ops))

	for _, op := range ops {
		if err := checkPath(op.Path); err != nil {
			return nil, err
		}
		old, existed, err := w.Read(op.Path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", op.Path, err)
		}

		switch op.Kind {
		case "add":
			if existed {
				// Refused rather than overwritten: an Add on an existing path is almost always a
				// caller that meant Update, and silently replacing the file would discard
				// content the patch never mentioned.
				return nil, fmt.Errorf("%s already exists; use Update File to change it", op.Path)
			}
			plan = append(plan, resolved{op: op, content: []byte(op.Content), existed: false})

		case "delete":
			if !existed {
				return nil, fmt.Errorf("%s does not exist, so it cannot be deleted", op.Path)
			}
			plan = append(plan, resolved{op: op, existed: true})

		case "update":
			if !existed {
				return nil, fmt.Errorf("%s does not exist; use Add File to create it", op.Path)
			}
			newContent, err := ApplyHunks(string(old), op.Hunks)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", op.Path, err)
			}
			plan = append(plan, resolved{op: op, content: []byte(newContent), existed: true})

		default:
			return nil, fmt.Errorf("unknown operation %q for %s", op.Kind, op.Path)
		}
	}

	// Validation passed: commit the writes.
	for _, r := range plan {
		switch r.op.Kind {
		case "add", "update":
			w.Write(r.op.Path, r.content)
		case "delete":
			w.Remove(r.op.Path)
		}
	}
	return ops, nil
}

// checkPath refuses anything that could escape the workspace or is not a plain relative path.
func checkPath(p string) error {
	if strings.TrimSpace(p) == "" {
		return fmt.Errorf("a patch section has an empty path")
	}
	if strings.HasPrefix(p, "/") {
		return fmt.Errorf("path %q must be relative to the repository root", p)
	}
	// Checked as path SEGMENTS: a file legitimately named "a..b" is fine, "a/../b" is not.
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return fmt.Errorf("path %q may not contain ..", p)
		}
	}
	if strings.ContainsAny(p, "\x00") {
		return fmt.Errorf("path contains a NUL byte")
	}
	return nil
}
