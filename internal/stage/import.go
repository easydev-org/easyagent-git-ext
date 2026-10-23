package stage

import (
	"context"
	"fmt"

	"github.com/easylab-platform/easyvcs/gitbridge"
	"github.com/easylab-platform/easyvcs/revision"
)

// ImportParams describes a remote to bring into the store.
type ImportParams struct {
	Tenant string
	// Dest is the git URL to read from.
	Dest string
	// Branch is the branch to import (empty = the remote's default).
	Branch string
	// Token authenticates a private remote; empty imports anonymously.
	Token string
}

// ImportResult reports what the import did.
type ImportResult struct {
	// Revisions is how many NEW revisions the import produced. Zero means the content was
	// already present, which is the common case on a second call.
	Revisions int
	// Total is how many revisions the store holds afterwards.
	Total int
}

// Import reads a remote branch into the store.
//
// Import is what makes the stage usable from nothing: the tools read stored content, so
// there has to be stored content. It is deliberately a separate step rather than an
// implicit first read — importing a large repository takes time, and a caller asking for
// one file should not trigger it by surprise.
//
// Idempotent: importing an already-imported branch adds nothing and reports zero.
func (s *Stage) Import(ctx context.Context, p ImportParams) (ImportResult, error) {
	repo, err := s.ensureRepo(p.Tenant)
	if err != nil {
		return ImportResult{}, err
	}
	ws := revision.NewWorkspace(repo)

	before, err := ws.Log()
	if err != nil {
		return ImportResult{}, fmt.Errorf("list revisions: %w", err)
	}

	if _, err := gitbridge.ImportBranch(ws, repo, gitbridge.ImportOptions{
		Source: p.Dest,
		Branch: p.Branch,
		Token:  p.Token,
	}); err != nil {
		return ImportResult{}, err
	}

	after, err := ws.Log()
	if err != nil {
		return ImportResult{}, fmt.Errorf("list revisions after import: %w", err)
	}
	return ImportResult{Revisions: len(after) - len(before), Total: len(after)}, nil
}
