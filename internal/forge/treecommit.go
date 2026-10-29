package forge

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// TreeChange is one file to write in a commit.
type TreeChange struct {
	Path    string
	Content []byte
}

/**
 * CommitFiles writes several files in ONE commit on one branch.
 *
 * THE BATCH ENDPOINT IS `POST /repos/{owner}/{repo}/contents`, which is easy to mistake for the
 * single-file one next to it: its summary is "Modify multiple files in a repository", and its body
 * carries a `files` ARRAY. One call, one commit, every file in it.
 *
 * That distinction is the whole point. `PUT /contents/{filepath}` commits once per file, so a patch
 * touching five files would land as five commits — the log would say the change arrived in pieces, a
 * bisect would stop inside it, and reverting would take five reverts. Here "this patch landed" is a
 * single fact in the history.
 *
 * The `sha` on an update or a delete is an OPTIMISTIC LOCK: the forge refuses the whole call if the
 * file has moved since it was read, so a concurrent edit fails loudly instead of being overwritten.
 * That is why the current sha is fetched here rather than taken from the caller.
 */
func (c *Client) CommitFiles(ctx context.Context, branch, message string, changes []TreeChange, deletePaths []string) (string, error) {
	if branch == "" {
		return "", fmt.Errorf("branch is required")
	}
	if len(changes) == 0 && len(deletePaths) == 0 {
		return "", fmt.Errorf("nothing to commit")
	}

	// AN EMPTY REPOSITORY CANNOT BE WRITTEN AT ALL, and the failure it produces says nothing useful:
	// the contents endpoint answers HTTP 500 with an EMPTY body, so a caller sees "the server broke"
	// when the fact is "this repository has no commits yet". That state is easy to reach and easy to
	// mistake for a healthy repository — the forge's own migrate API leaves exactly this behind when
	// it fails part-way — so it is checked and named here rather than left to a 500.
	if empty, err := c.isEmpty(ctx); err == nil && empty {
		return "", fmt.Errorf(
			"the repository is EMPTY (no commits yet), so it cannot be written. "+
				"Give it an initial commit first — or seed it from the upstream with "+
				"git-branch-create from=\"upstream:%s\" — then retry", branch)
	}

	// Existing shas, needed for every update and delete. Read in ONE listing rather than per file:
	// the batch call takes all of them, and a per-file GET would be N round trips for data this
	// already has.
	existing, err := c.fileSHAs(ctx, branch)
	if err != nil {
		return "", err
	}

	ops := make([]map[string]any, 0, len(changes)+len(deletePaths))
	for _, ch := range changes {
		path := strings.TrimPrefix(strings.TrimSpace(ch.Path), "/")
		if path == "" {
			return "", fmt.Errorf("a change has an empty path")
		}
		op := map[string]any{
			"path":    path,
			"content": base64.StdEncoding.EncodeToString(ch.Content),
		}
		if sha, ok := existing[path]; ok {
			// The file is there: this is an update, and the sha is what makes it safe.
			op["operation"] = "update"
			op["sha"] = sha
		} else {
			op["operation"] = "create"
		}
		ops = append(ops, op)
	}
	for _, p := range deletePaths {
		path := strings.TrimPrefix(strings.TrimSpace(p), "/")
		if path == "" {
			continue
		}
		sha, ok := existing[path]
		if !ok {
			// Already gone. Deleting it again is not an error the caller can act on, and the batch
			// call would fail the WHOLE patch for it.
			continue
		}
		ops = append(ops, map[string]any{"operation": "delete", "path": path, "sha": sha})
	}
	if len(ops) == 0 {
		return "", fmt.Errorf("nothing to commit: every path was already absent")
	}

	var out struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	if err := c.do(ctx, http.MethodPost, "/repos/"+c.path()+"/contents", map[string]any{
		"branch":  branch,
		"message": message,
		"files":   ops,
	}, &out); err != nil {
		return "", err
	}
	return out.Commit.SHA, nil
}

/**
 * fileSHAs maps every file path at `branch` to its blob sha.
 *
 * Recursive, because a patch may touch a nested path and the update operation needs that file's sha
 * — a listing of the root alone would not have it. Trees come back without recursion on some forges,
 * so the recursive form is requested explicitly.
 */
func (c *Client) fileSHAs(ctx context.Context, branch string) (map[string]string, error) {
	var tree struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
			SHA  string `json:"sha"`
		} `json:"tree"`
	}
	if err := c.do(ctx, http.MethodGet,
		"/repos/"+c.path()+"/git/trees/"+esc(branch)+"?recursive=true&per_page=10000", nil, &tree); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(tree.Tree))
	for _, e := range tree.Tree {
		if e.Type == "blob" {
			out[e.Path] = e.SHA
		}
	}
	return out, nil
}

// path is the repository's API path segment.
func (c *Client) path() string { return esc(c.Owner) + "/" + esc(c.Repo) }

/**
 * isEmpty reports whether the repository has no commits.
 *
 * A FAILURE TO FIND OUT is not an error the caller should see: this check exists to improve a
 * message, and refusing to commit because the probe failed would turn a diagnostic into a blocker.
 * The caller treats an error from here as "carry on", which is why the return is (bool, error) and
 * the caller ignores the error rather than propagating it.
 */
func (c *Client) isEmpty(ctx context.Context) (bool, error) {
	var out struct {
		Empty bool `json:"empty"`
	}
	if err := c.do(ctx, http.MethodGet, "/repos/"+c.path(), nil, &out); err != nil {
		return false, err
	}
	return out.Empty, nil
}

/**
 * CreateBranch creates `branch` at the head of `from`.
 *
 * A branch that already exists is reported as ErrBranchExists rather than as a failure: a caller
 * preparing a working branch wants "make sure this exists", not "create it or tell me somebody
 * already did".
 */
func (c *Client) CreateBranch(ctx context.Context, branch, from string) error {
	if branch == "" || from == "" {
		return fmt.Errorf("branch and from are required")
	}
	err := c.do(ctx, http.MethodPost, "/repos/"+c.path()+"/branches", map[string]any{
		"new_branch_name": branch,
		"old_branch_name": from,
	}, nil)
	if err != nil && strings.Contains(err.Error(), "409") {
		return ErrBranchExists
	}
	return err
}

// ErrBranchExists means the branch was already there.
var ErrBranchExists = fmt.Errorf("branch already exists")

/**
 * BranchExists reports whether a branch is there, and the commit it points at.
 *
 * The commit comes back too because a caller deciding what to do about a name collision needs to see
 * what it would be reusing — "already exists" alone gives them nothing to judge by.
 */
func (c *Client) BranchExists(ctx context.Context, branch string) (bool, string, error) {
	if branch == "" {
		return false, "", fmt.Errorf("branch is required")
	}
	var out struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	err := c.do(ctx, http.MethodGet, "/repos/"+c.path()+"/branches/"+esc(branch), nil, &out)
	if err != nil {
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "not found") {
			return false, "", nil
		}
		return false, "", err
	}
	return true, out.Commit.ID, nil
}

/**
 * DeleteBranch removes a branch.
 *
 * Only ever called for a branch this extension created, and only when the caller asked for `reset`:
 * losing somebody else's commits is not something a convenience flag should be able to do quietly.
 */
func (c *Client) DeleteBranch(ctx context.Context, branch string) error {
	if branch == "" {
		return fmt.Errorf("branch is required")
	}
	err := c.do(ctx, http.MethodDelete, "/repos/"+c.path()+"/branches/"+esc(branch), nil, nil)
	if err != nil && (strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "not found")) {
		// Already gone. Deleting it again is not a failure the caller can act on.
		return nil
	}
	return err
}

// do performs one API call, decoding a JSON reply into out when out is not nil.
func (c *Client) do(ctx context.Context, method, apiPath string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.apiBase+apiPath, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "token "+c.token)
	}
	cl := &http.Client{Timeout: 60 * time.Second}
	res, err := cl.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, apiPath, err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<22))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("%s %s: HTTP %d: %s", method, apiPath, res.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("%s %s: unreadable answer: %w", method, apiPath, err)
		}
	}
	return nil
}

// esc escapes one path segment.
func esc(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.', r == '~':
			b.WriteRune(r)
		default:
			b.WriteString(fmt.Sprintf("%%%02X", r))
		}
	}
	return b.String()
}
