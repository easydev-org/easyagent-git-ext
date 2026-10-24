package workspace

import (
	"fmt"
	"strings"
)

// Applying a parsed patch to a workspace's files.
//
// This is the ONLY way a session changes files. There is no write/delete/move trio: one way to
// edit means one way to get it wrong, and a patch shows the intent where resending a whole file
// does not. The same grammar the sandbox file tools use, so a model that has learned it there
// already knows it here.

// FileStore is what ApplyPatch needs. Narrow on purpose, so the patch logic can be tested
// against an in-memory store and states exactly what applying a patch may touch.
type FileStore interface {
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, content []byte) error
	RemoveFile(path string) error
}

// ApplyPatch applies a patch to the workspace as ONE unit.
//
// Every operation is validated against the CURRENT content before anything is written, so a
// patch whose third hunk does not apply leaves the tree untouched rather than half-edited. A
// partially applied patch is worse than a rejected one, because the caller believes the edit
// landed and a later diff cannot tell which half did.
func (w *Workspace) ApplyPatch(text string) ([]PatchOp, error) {
	ops, err := ParsePatch(text)
	if err != nil {
		return nil, err
	}
	if len(ops) == 0 {
		return nil, fmt.Errorf("the patch is empty (no Add/Update/Delete sections)")
	}

	type resolved struct {
		op      PatchOp
		content []byte
	}
	plan := make([]resolved, 0, len(ops))

	for _, op := range ops {
		// safePath runs here as well as inside Read/Write: a refusal must happen during
		// VALIDATION so a rejected path leaves nothing written.
		if _, err := w.safePath(op.Path); err != nil {
			return nil, err
		}
		old, rerr := w.ReadFile(op.Path)
		existed := rerr == nil

		switch op.Kind {
		case "add":
			if existed {
				// Refused rather than overwritten: an Add on an existing path is almost always
				// a caller that meant Update, and replacing the file would discard content the
				// patch never mentioned.
				return nil, fmt.Errorf("%s already exists; use Update File to change it", op.Path)
			}
			plan = append(plan, resolved{op: op, content: []byte(op.Content)})

		case "delete":
			if !existed {
				return nil, fmt.Errorf("%s does not exist, so it cannot be deleted", op.Path)
			}
			plan = append(plan, resolved{op: op})

		case "update":
			if !existed {
				return nil, fmt.Errorf("%s does not exist; use Add File to create it", op.Path)
			}
			newContent, aerr := ApplyHunks(string(old), op.Hunks)
			if aerr != nil {
				return nil, fmt.Errorf("%s: %w", op.Path, aerr)
			}
			plan = append(plan, resolved{op: op, content: []byte(newContent)})

		default:
			return nil, fmt.Errorf("unknown operation %q for %s", op.Kind, op.Path)
		}
	}

	// Validation passed: commit the writes.
	for _, r := range plan {
		switch r.op.Kind {
		case "add", "update":
			if err := w.WriteFile(r.op.Path, r.content); err != nil {
				return nil, err
			}
		case "delete":
			if err := w.RemoveFile(r.op.Path); err != nil {
				return nil, err
			}
		}
	}
	return ops, nil
}

// ApplyPatchTo applies a patch to an arbitrary FileStore. Used by the tests.
func ApplyPatchTo(s FileStore, text string) ([]PatchOp, error) {
	ops, err := ParsePatch(text)
	if err != nil {
		return nil, err
	}
	if len(ops) == 0 {
		return nil, fmt.Errorf("the patch is empty (no Add/Update/Delete sections)")
	}
	type resolved struct {
		op      PatchOp
		content []byte
		kind    string
	}
	plan := make([]resolved, 0, len(ops))

	for _, op := range ops {
		if strings.TrimSpace(op.Path) == "" {
			return nil, fmt.Errorf("a patch section has an empty path")
		}
		old, rerr := s.ReadFile(op.Path)
		existed := rerr == nil
		switch op.Kind {
		case "add":
			if existed {
				return nil, fmt.Errorf("%s already exists; use Update File to change it", op.Path)
			}
			plan = append(plan, resolved{op: op, content: []byte(op.Content), kind: "add"})
		case "delete":
			if !existed {
				return nil, fmt.Errorf("%s does not exist, so it cannot be deleted", op.Path)
			}
			plan = append(plan, resolved{op: op, kind: "delete"})
		case "update":
			if !existed {
				return nil, fmt.Errorf("%s does not exist; use Add File to create it", op.Path)
			}
			nc, aerr := ApplyHunks(string(old), op.Hunks)
			if aerr != nil {
				return nil, fmt.Errorf("%s: %w", op.Path, aerr)
			}
			plan = append(plan, resolved{op: op, content: []byte(nc), kind: "update"})
		default:
			return nil, fmt.Errorf("unknown operation %q for %s", op.Kind, op.Path)
		}
	}
	for _, r := range plan {
		switch r.kind {
		case "add", "update":
			if err := s.WriteFile(r.op.Path, r.content); err != nil {
				return nil, err
			}
		case "delete":
			if err := s.RemoveFile(r.op.Path); err != nil {
				return nil, err
			}
		}
	}
	return ops, nil
}
