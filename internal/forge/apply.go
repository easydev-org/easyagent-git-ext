package forge

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"
)

// Applying a parsed patch to a repository.
//
// This is the ONLY way a session changes files. There is no write/delete/move trio: one way to edit
// means one way to get it wrong, and a patch shows the intent where resending a whole file does not.
// The grammar is the same one the sandbox file tools use, so a model that has learned it there
// already knows it here.

// FileStore is what the patch logic needs: read one file. Writes go through the plan, not through
// this interface, because a forge cannot commit one file at a time without fragmenting the history.
type FileStore interface {
	ReadFile(path string) ([]byte, error)
}

// Change is one file operation resolved from a patch, ready to be committed.
type Change struct {
	// Kind is "add", "update" or "delete".
	Kind string
	Path string
	// Content is the new file, for add and update. Nil for delete.
	Content []byte
}

/**
 * PlanPatch reads every file a patch touches and resolves the result, WITHOUT writing anything.
 *
 * WHY IT STOPS AT A PLAN: a forge commits a whole change set at once
 * (`POST /contents` with a `files` array), so the writes are not this function's business. Splitting
 * it this way is also what makes the patch ATOMIC for free: an invalid hunk in the third operation
 * means the plan never reaches the commit, so the repository is untouched rather than half-edited —
 * and a half-edited tree is worse than a rejected patch, because the caller believes the edit landed.
 *
 * `Add` on an existing path is REFUSED rather than treated as an update: that is almost always a
 * caller who meant Update, and replacing the file would discard content the patch never mentioned.
 * `Update` or `Delete` on a missing path is refused for the mirror-image reason.
 */
func PlanPatch(s FileStore, text string) ([]Change, error) {
	ops, err := ParsePatch(text)
	if err != nil {
		return nil, err
	}
	if len(ops) == 0 {
		return nil, fmt.Errorf("the patch is empty (no Add/Update/Delete sections)")
	}

	plan := make([]Change, 0, len(ops))
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
			plan = append(plan, Change{Kind: "add", Path: op.Path, Content: []byte(op.Content)})

		case "delete":
			if !existed {
				return nil, fmt.Errorf("%s does not exist, so it cannot be deleted", op.Path)
			}
			plan = append(plan, Change{Kind: "delete", Path: op.Path})

		case "update":
			if !existed {
				return nil, fmt.Errorf("%s does not exist; use Add File to create it", op.Path)
			}
			nc, aerr := ApplyHunks(string(old), op.Hunks)
			if aerr != nil {
				return nil, fmt.Errorf("%s: %w", op.Path, aerr)
			}
			plan = append(plan, Change{Kind: "update", Path: op.Path, Content: []byte(nc)})

		default:
			return nil, fmt.Errorf("unknown operation %q for %s", op.Kind, op.Path)
		}
	}
	return plan, nil
}

// ApplyPatch plans a patch and commits it as ONE commit on `branch`.
//
// The message is required: a commit with an empty message is a commit nobody can read in a log, and
// the caller always knows why it is changing the file.
func (c *Client) ApplyPatch(ctx context.Context, branch, message, text string) ([]Change, string, error) {
	plan, err := PlanPatch(forgeFileStore{c: c, branch: branch}, text)
	if err != nil {
		return nil, "", err
	}
	changes := make([]TreeChange, 0, len(plan))
	deletes := make([]string, 0, len(plan))
	for _, ch := range plan {
		if ch.Kind == "delete" {
			deletes = append(deletes, ch.Path)
			continue
		}
		changes = append(changes, TreeChange{Path: ch.Path, Content: ch.Content})
	}
	sha, err := c.CommitFiles(ctx, branch, message, changes, deletes)
	if err != nil {
		return nil, "", err
	}
	return plan, sha, nil
}

// forgeFileStore reads files from one branch of one repository, for the patch planner.
type forgeFileStore struct {
	c      *Client
	branch string
}

// ReadFile returns the file's content at the branch, or an error when it is not there.
//
// The ERROR is how "does not exist" is expressed — the planner only ever asks whether it errored —
// but the message still names the path, because a caller that sees it in a tool result needs to know
// which file was missing.
func (f forgeFileStore) ReadFile(path string) ([]byte, error) {
	var out struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	apiPath := "/repos/" + f.c.path() + "/contents/" + escapePathSegments(path) + "?ref=" + esc(f.branch)
	// A timeout of its own: the planner may read several files, and one that never answers must not
	// hold the whole patch.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := f.c.do(ctx, "GET", apiPath, nil, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if out.Encoding == "base64" {
		// The API wraps its base64 at 60 characters. StdEncoding rejects the newlines, so they are
		// removed first — this is the documented shape of the response, not a defensive guess.
		clean := strings.NewReplacer("\n", "", "\r", "").Replace(out.Content)
		dec, derr := base64.StdEncoding.DecodeString(clean)
		if derr != nil {
			return nil, fmt.Errorf("%s: undecodable content: %w", path, derr)
		}
		return dec, nil
	}
	return []byte(out.Content), nil
}

// escapePathSegments escapes each segment of a FILE path, keeping the slashes.
//
// Escaping the whole path would turn "src/lib/x.go" into one opaque segment the API cannot descend
// into; escaping none of it would let a segment containing a slash-shaped character reshape the URL.
func escapePathSegments(p string) string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	for i, s := range parts {
		parts[i] = esc(s)
	}
	return strings.Join(parts, "/")
}
