package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/easydev-org/easybus-sdk/extension"

	"github.com/easydev-org/easyagent-gitdev-ext/internal/ghclient"
)

// Artifacts and run control: the tools that DO something to CI, not just report on it.

// artifactList lists a run's artifacts.
func (t *toolset) artifactList(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	Progress(ctx).Phase("listing artifacts")
	c, repo, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	runID := int64(argInt(args, "run"))
	if runID <= 0 {
		return extension.ToolResultData{}, errArg(`"run" is required: the run id from gh-run-list`)
	}
	items, lerr := c.ListArtifacts(ctx, runID)
	if lerr != nil {
		return extension.ToolResultData{}, errFailed("%s run %d: %v", repo, runID, lerr)
	}
	if len(items) == 0 {
		return extension.ToolResultData{
			Content: fmt.Sprintf("run %d uploaded no artifacts", runID),
			Data:    map[string]any{"artifacts": items, "count": 0},
		}, nil
	}
	var b strings.Builder
	for _, a := range items {
		state := ""
		if a.Expired {
			// An expired artifact cannot be downloaded, so saying so here is what stops a caller
			// from trying and getting a confusing failure.
			state = "  EXPIRED"
		}
		fmt.Fprintf(&b, "%-12d %-40s %8s  %s%s\n", a.ID, a.Name, humanSize(a.Size), a.CreatedAt, state)
	}
	fmt.Fprintf(&b, "\n%d artifact(s)", len(items))
	return extension.ToolResultData{
		Content: b.String(),
		Data:    map[string]any{"artifacts": items, "count": len(items)},
	}, nil
}

// artifactDownload downloads one artifact and stores it in the tenant's object storage.
//
// WHY INTO OBJECT STORAGE, not into the response: an artifact is a zip that can be megabytes or
// gigabytes, and base64 in a tool result would be both useless to read and ruinous to the
// conversation. Storing it returns a handle the session can hand to another tool — the same idea
// as sandbox-file-upload, and the same storage.
func (t *toolset) artifactDownload(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	Progress(ctx).Phase("downloading the artifact")
	c, repo, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	id := int64(argInt(args, "artifact"))
	if id <= 0 {
		return extension.ToolResultData{}, errArg(`"artifact" is required: the artifact id from gh-artifacts`)
	}
	if t.deps.PutObject == nil {
		return extension.ToolResultData{}, &extension.TypedError{
			Code: easybusInternal, Message: "this deployment has no object storage",
		}
	}

	maxBytes := int64(argInt(args, "max_bytes"))
	if maxBytes <= 0 {
		maxBytes = 64 << 20
	}
	data, derr := c.DownloadArtifact(ctx, id, maxBytes)
	if derr != nil {
		return extension.ToolResultData{}, errFailed("%s artifact %d: %v", repo, id, derr)
	}

	// The run id is part of the key because GitHub lets each run upload an artifact with the SAME
	// name; without it one run's build-output would overwrite another's.
	name := fmt.Sprintf("github/artifacts/%s/%d-%s.zip",
		strings.ReplaceAll(repo, "/", "__"), id, sanitiseName(argStr(args, "name")))
	Progress(ctx).Phase("storing the artifact")
	if perr := t.deps.PutObject(ctx, tenant, name, data); perr != nil {
		return extension.ToolResultData{}, errFailed("store artifact: %v", perr)
	}

	return extension.ToolResultData{
		Content: fmt.Sprintf("downloaded artifact %d (%s), stored as %s", id, humanSize(int64(len(data))), name),
		Data:    map[string]any{"artifact": id, "object": name, "bytes": len(data)},
	}, nil
}

// runRerun reruns a run, or only its failed jobs.
func (t *toolset) runRerun(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	Progress(ctx).Phase("requesting the rerun")
	c, repo, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	runID := int64(argInt(args, "run"))
	if runID <= 0 {
		return extension.ToolResultData{}, errArg(`"run" is required: the run id from gh-run-list`)
	}
	// Default is failed-only: a rerun is almost always "the flaky one failed", and rerunning
	// everything throws away the jobs that already passed.
	failedOnly := argBoolDefault(args, "failed_only", true)
	if failedOnly {
		if rerr := c.RerunFailedJobs(ctx, runID); rerr != nil {
			return extension.ToolResultData{}, errFailed("%s run %d: %v", repo, runID, rerr)
		}
		return extension.ToolResultData{
			Content: fmt.Sprintf("rerunning the failed jobs of run %d\n\n"+
				"A rerun starts a new ATTEMPT on the same run; watch it with gh-run-jobs", runID),
			Data: map[string]any{"run": runID, "failed_only": true},
		}, nil
	}
	if rerr := c.RerunRun(ctx, runID); rerr != nil {
		return extension.ToolResultData{}, errFailed("%s run %d: %v", repo, runID, rerr)
	}
	return extension.ToolResultData{
		Content: fmt.Sprintf("rerunning the whole of run %d", runID),
		Data:    map[string]any{"run": runID, "failed_only": false},
	}, nil
}

// runCancel cancels a run.
func (t *toolset) runCancel(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	Progress(ctx).Phase("requesting cancellation")
	c, repo, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	runID := int64(argInt(args, "run"))
	if runID <= 0 {
		return extension.ToolResultData{}, errArg(`"run" is required: the run id from gh-run-list`)
	}
	if cerr := c.CancelRun(ctx, runID); cerr != nil {
		return extension.ToolResultData{}, errFailed("%s run %d: %v", repo, runID, cerr)
	}
	return extension.ToolResultData{
		Content: fmt.Sprintf("cancelling run %d\n\n"+
			"Cancellation is a REQUEST, not an instant stop: GitHub lets the step in progress finish. "+
			"Poll gh-run-jobs until the job reports completed.", runID),
		Data: map[string]any{"run": runID},
	}, nil
}

// runDispatch triggers a workflow_dispatch event.
func (t *toolset) runDispatch(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	Progress(ctx).Phase("resolving the ref")
	c, repo, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	workflow, err := required(args, "workflow")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	ref := argStr(args, "ref")
	if ref == "" {
		// GitHub refuses a dispatch with no ref; resolving the default branch here turns that
		// refusal into the right behaviour instead of an error the caller must decode.
		info, ierr := c.Info(ctx)
		if ierr != nil {
			return extension.ToolResultData{}, errFailed("resolve default branch: %v", ierr)
		}
		ref = info.DefaultBranch
	}
	inputs := map[string]any{}
	if raw, ok := args["inputs"].(map[string]any); ok {
		inputs = raw
	}
	Progress(ctx).Phase("dispatching")
	runID, url, derr := c.DispatchRun(ctx, ghclient.DispatchOptions{
		Workflow: workflow, Ref: ref, Inputs: inputs,
	})
	if derr != nil {
		return extension.ToolResultData{}, errFailed("%s %s: %v", repo, workflow, derr)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "dispatched %s on %s\n", workflow, ref)
	if runID > 0 {
		fmt.Fprintf(&b, "run: %d\n%s\n\nwatch it with gh-run-jobs run=%d", runID, url, runID)
	} else {
		// No run id came back: say that, rather than printing "run 0".
		b.WriteString("GitHub accepted the dispatch but returned no run id; find it with gh-run-list\n")
	}
	return extension.ToolResultData{
		Content: b.String(),
		Data:    map[string]any{"workflow": workflow, "ref": ref, "run": runID, "url": url},
	}, nil
}

// sanitiseName reduces an artifact name to something safe for an object key.
func sanitiseName(s string) string {
	if s == "" {
		return "artifact"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if len(out) > 80 {
		out = out[:80]
	}
	return out
}

// humanSize renders a byte count the way a person reads it.
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}
