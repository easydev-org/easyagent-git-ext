// Package sandbox drives git operations on a remote easyworker: it selects a
// sandbox from config, verifies the target path is a git repository, and runs
// `git` there via the worker's Execute RPC (git runs IN the sandbox).
package sandbox

import (
	"context"
	"fmt"
	"strings"
	"time"

	workerv1 "github.com/easydev-org/easyagent-git-ext/gen/worker/v1"
	easybus "github.com/easydev-org/easybus-sdk"
	"github.com/easydev-org/easybus-sdk/extension"
)

// Client is the worker.v1 Connect client (a type alias to the generated one).
type Client = WorkerClient

// Runner runs commands against one sandbox.
type Runner struct {
	cl   WorkerClient
	name string
}

// NewRunner builds a Runner for a sandbox endpoint.
func NewRunner(cache *ClientCache, ep Endpoint, name string) *Runner {
	return &Runner{cl: cache.Worker(ep), name: name}
}

// Run executes `command` in `workdir` (relative to the workspace root) and
// returns the combined output. A non-zero exit is an error carrying the output.
func (r *Runner) Run(ctx context.Context, workdir, command string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	started, err := r.cl.Execute(ctx, &workerv1.ExecuteRequest{Command: command, Workdir: workdir, TimeoutMs: int32(timeout.Milliseconds())})
	if err != nil {
		return "", &extension.TypedError{Code: easybus.ToolResultErrorCodeRetryable, Message: fmt.Sprintf("sandbox execute: %v", err)}
	}
	jobID := started.GetJobId()
	deadline := time.Now().Add(timeout)
	for {
		slice := time.Until(deadline)
		if slice > 600*time.Second {
			slice = 600 * time.Second
		}
		if slice < time.Millisecond {
			slice = time.Millisecond
		}
		res, err := r.cl.JobWait(ctx, &workerv1.JobWaitRequest{JobId: jobID, TimeoutMs: int32(slice.Milliseconds())})
		if err != nil {
			return "", &extension.TypedError{Code: easybus.ToolResultErrorCodeRetryable, Message: fmt.Sprintf("job wait: %v", err)}
		}
		if res.GetState() != "running" {
			out := r.output(ctx, jobID)
			if res.GetExitCode() != 0 {
				return out, &extension.TypedError{Code: easybus.ToolResultErrorCodeBusiness, Message: strings.TrimSpace(out)}
			}
			return out, nil
		}
		if time.Now().After(deadline) {
			return r.output(ctx, jobID), &extension.TypedError{Code: easybus.ToolResultErrorCodeRetryable, Message: "command timed out"}
		}
	}
}

// output reads the full job output.
func (r *Runner) output(ctx context.Context, jobID string) string {
	res, err := r.cl.JobOutput(ctx, &workerv1.JobOutputRequest{JobId: jobID, Stream: "all"})
	if err != nil {
		return ""
	}
	return strings.Join(res.GetLines(), "\n")
}

// EnsureRepo verifies that `path` (relative to the workspace root) is a git
// repository, returning a clear error otherwise. Every git-* tool calls this
// first, so a non-repo path is rejected up front.
func (r *Runner) EnsureRepo(ctx context.Context, path string) error {
	if path == "" {
		return &extension.TypedError{Code: easybus.ToolResultErrorCodeInvalidArgument, Message: "path is required (the git repository directory in the sandbox)"}
	}
	cmd := fmt.Sprintf("git -C %s rev-parse --git-dir", shellQuote(path))
	out, err := r.Run(ctx, "", cmd, 30*time.Second)
	if err != nil {
		return &extension.TypedError{
			Code:    easybus.ToolResultErrorCodeInvalidArgument,
			Message: fmt.Sprintf("%s is not a git repository (run git-clone first): %s", path, strings.TrimSpace(out)),
		}
	}
	return nil
}

// Git runs `git -C <path> <args...>` in the sandbox.
func (r *Runner) Git(ctx context.Context, path string, timeout time.Duration, args ...string) (string, error) {
	parts := make([]string, 0, len(args)+3)
	parts = append(parts, "git", "-C", shellQuote(path))
	for _, a := range args {
		parts = append(parts, a)
	}
	return r.Run(ctx, "", strings.Join(parts, " "), timeout)
}

// shellQuote single-quotes a path for the sandbox shell (mvdan-sh POSIX).
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
