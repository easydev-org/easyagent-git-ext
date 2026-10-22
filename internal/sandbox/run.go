// Package sandbox drives git operations on a remote easyworker: it selects a
// sandbox from config, verifies the target path is a git repository, and runs
// `git` there via the worker's Execute RPC (git runs IN the sandbox).
package sandbox

import (
	"context"
	"fmt"
	"strings"
	"time"

	workerv1 "github.com/easydev-org/easyagent-gitea-ext/gen/worker/v1"
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
	return r.wait(ctx, started.GetJobId(), timeout)
}

// RunSecret runs a command that reads a one-line SECRET from stdin (into the
// shell variable GIT_TOKEN) and then performs the git operation.
//
// The secret NEVER appears in `command` (the worker persists Job.Command to
// sqlite, and ListJobs would expose it) nor in the Execute env (which
// /proc/<pid>/environ exposes). It rides the in-memory stdin pipe only, and the
// command unsets it right after the git call. This is the "no token at rest"
// contract: the secret is never written to disk, logs, or the job history.
func (r *Runner) RunSecret(ctx context.Context, workdir, command, secret string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	started, err := r.cl.Execute(ctx, &workerv1.ExecuteRequest{Command: command, Workdir: workdir, TimeoutMs: int32(timeout.Milliseconds())})
	if err != nil {
		return "", &extension.TypedError{Code: easybus.ToolResultErrorCodeRetryable, Message: fmt.Sprintf("sandbox execute: %v", err)}
	}
	jobID := started.GetJobId()
	// Feed the secret on stdin. JobStdin blocks until the command reads it (the
	// worker's stdin is an unbuffered os.Pipe), so do it off the wait loop; a
	// command that never reads simply sees the pipe close when the job ends.
	go func() {
		_, _ = r.cl.JobStdin(ctx, &workerv1.JobStdinRequest{JobId: jobID, Data: []byte(secret + "\n"), Close: true})
	}()
	return r.wait(ctx, jobID, timeout)
}

// wait blocks until the job leaves "running" (or the budget expires) and
// returns its output.
func (r *Runner) wait(ctx context.Context, jobID string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	// Relay output while we wait.
	//
	// `wait` polls JobWait, which tells us nothing until the command ends — so a
	// `git clone` of a large repo showed "running" for a minute and then all the
	// output at once. A WatchJob stream runs alongside purely to forward lines;
	// the poll loop below remains the authority on WHEN the command finished, so
	// nothing about the wait semantics changes.
	stopRelay := r.relay(ctx, jobID)
	defer stopRelay()
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

// RemoteURL returns the URL of the "origin" remote of the repo at path (” when
// absent). Used to resolve the credential for pull/push.
func (r *Runner) RemoteURL(ctx context.Context, path string) string {
	out, err := r.Git(ctx, path, 30*time.Second, "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// relay forwards a running job's output to the console until the job ends.
//
// Best-effort by design: if the worker cannot be watched, the command still runs
// and its result is unaffected — only the live view is lost.
func (r *Runner) relay(ctx context.Context, jobID string) func() {
	prog := NewProgress()
	relayCtx, cancel := context.WithCancel(ctx)
	go func() {
		defer prog.Flush(ctx)
		stream, err := r.cl.WatchJob(relayCtx, &workerv1.WatchJobRequest{JobId: jobID})
		if err != nil {
			return
		}
		defer stream.Close()
		for {
			msg, rerr := stream.Receive()
			if rerr != nil {
				return
			}
			if out := msg.GetOutput(); out != "" {
				prog.Add(relayCtx, out)
			}
			if d := msg.GetDone(); d != nil {
				prog.Flush(relayCtx)
				return
			}
		}
	}()
	return func() { cancel() }
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
		parts = append(parts, quoteIfNeeded(a))
	}
	return r.Run(ctx, "", strings.Join(parts, " "), timeout)
}

// quoteIfNeeded quotes an argument for the sandbox shell.
//
// Callers already shell-quote values they interpolate (paths, refs, messages),
// and quoting those AGAIN produced `”E2E.md”` — a single-quoted empty string
// followed by the literal text, which git reads as a pathspec of
// `'E2E.md'` and rejects with "did not match any files". Everything else (flags,
// plain refs) is left untouched so the command stays readable in logs.
func quoteIfNeeded(a string) string {
	// Already single-quoted by the caller: leave it exactly as it is.
	if len(a) >= 2 && strings.HasPrefix(a, "'") && strings.HasSuffix(a, "'") {
		return a
	}
	return quoteArg(a)
}

// shellQuote single-quotes a path for the sandbox shell (mvdan-sh POSIX).
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// quoteArg quotes a git argument only when it contains a shell metacharacter,
// so plain flags/refs stay readable but "(%an)" and paths are safe.
func quoteArg(a string) string {
	if a == "" {
		return "''"
	}
	if strings.IndexFunc(a, func(r rune) bool {
		return !(r == '-' || r == '_' || r == '.' || r == '/' || r == ':' || r == '=' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'))
	}) < 0 {
		return a
	}
	return shellQuote(a)
}
