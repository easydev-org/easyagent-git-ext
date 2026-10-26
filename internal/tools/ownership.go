package tools

import (
	"context"
	"fmt"

	easybus "github.com/easydev-org/easybus-sdk"
	"github.com/easydev-org/easybus-sdk/extension"
)

// Ownership: "may this session act on THIS object, not merely in this repository?"
//
// WHY THIS IS SEPARATE FROM THE PERMISSION CHECK: a developer permission says "you may work here".
// It does not say "you may close somebody else's pull request". Those are different questions, and
// folding them together would mean every developer could veto every colleague's work.
//
// THE RULE, in one line: a developer may act on what it created; a maintainer may act on anything.
//
// GitHub offers exactly one field for this — the author login of the issue, pull request or
// comment — so ownership means "authored by the token's own identity". That is honest but narrow:
// it cannot express "this belongs to my team", and a session using a shared token is its own
// author for everything. Both limits are stated in the refusal so a caller is not misled.

// requireOwnership enforces "own work, unless maintainer".
//
// The repository-level permission is checked first, so a session with no grant at all gets the
// ordinary permission error rather than a confusing complaint about ownership.
func (t *toolset) requireOwnership(ctx context.Context, tenant, perm, repo string, number int) error {
	// A maintainer may act on anyone's object, so the ownership question never arises.
	if extension.Allowed(ctx, PermMaintainer, map[string]string{"repo": repo}) {
		return nil
	}
	if err := t.requireRepo(ctx, perm, repo); err != nil {
		return err
	}

	login, err := t.tokenLogin(ctx, tenant, repo)
	if err != nil || login == "" {
		// Without an identity the ownership question cannot be answered, and guessing "yes" would
		// let any developer act on anything. Refusing is the only safe answer.
		return &extension.TypedError{
			Code: easybus.ToolResultErrorCodePermissionDenied,
			Message: fmt.Sprintf("cannot tell who this session is, so it cannot act on #%d as its own; "+
				"a maintainer grant for %s would not need to ask", number, repo),
		}
	}

	author, aerr := t.issueAuthor(ctx, tenant, repo, number)
	if aerr != nil {
		return errFailed("look up the author of #%d: %v", number, aerr)
	}
	if author != login {
		return &extension.TypedError{
			Code: easybus.ToolResultErrorCodePermissionDenied,
			Message: fmt.Sprintf(
				"#%d was opened by %s, not by %s. A developer may only act on its own work; "+
					"acting on somebody else's needs repo.maintainer for %s",
				number, author, login, repo),
		}
	}
	return nil
}

// requireOwnershipOfRun enforces "own run, unless maintainer".
//
// Separate from requireOwnership because a workflow run has a different owner field: GitHub calls
// it triggering_actor, and it is who STARTED the run — not who wrote the workflow, and not who
// pushed the commit. That distinction matters when a maintainer re-runs a contributor's build.
func (t *toolset) requireOwnershipOfRun(ctx context.Context, tenant, repo string, runID int64) error {
	if extension.Allowed(ctx, PermMaintainer, map[string]string{"repo": repo}) {
		return nil
	}
	if err := t.requireRepo(ctx, PermDeveloper, repo); err != nil {
		return err
	}
	login, err := t.tokenLogin(ctx, tenant, repo)
	if err != nil || login == "" {
		return &extension.TypedError{
			Code: easybus.ToolResultErrorCodePermissionDenied,
			Message: fmt.Sprintf("cannot tell who this session is, so it cannot act on run %d as its own; "+
				"a maintainer grant for %s would not need to ask", runID, repo),
		}
	}
	actor, aerr := t.runActor(ctx, tenant, repo, runID)
	if aerr != nil {
		return errFailed("look up who triggered run %d: %v", runID, aerr)
	}
	if actor != login {
		return &extension.TypedError{
			Code: easybus.ToolResultErrorCodePermissionDenied,
			Message: fmt.Sprintf(
				"run %d was triggered by %s, not by %s. A developer may only act on its own runs; "+
					"acting on somebody else's needs repo.maintainer for %s",
				runID, actor, login, repo),
		}
	}
	return nil
}
