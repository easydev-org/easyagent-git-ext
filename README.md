# easyagent-gitea-ext

A standalone easyagent **extension server** that runs git operations —
`clone` / `status` / `log` / `diff` / `branch` / `checkout` / `add` / `commit` /
`pull` / `push` / `show` — **inside a remote easyworker sandbox**, over the
easybus protocol.

## Design

git runs **in the sandbox** (via the worker's `Execute` RPC), so this binary
needs **no system git of its own**. Every tool takes:

- **`sandbox`** — which easyworker to run in (see `list-sandboxes`); the first
  configured is the default.
- **`path`** — the repository directory in that sandbox (relative to the
  workspace root). **It must be a git repository**: every tool verifies it with
  `git -C <path> rev-parse --git-dir` and rejects the call otherwise.

This is why gitea-ext depends on the same sandbox model as
[`easyagent-sandbox-ext`](../easyagent-sandbox-ext): it **vendors the same
worker.v1 proto** and speaks the worker Connect contract directly.

## Configuration

Two SECRET knobs (edited from the agent's Extensions page):

- **`sandboxes`** — the easyworker list, `[{"name","url","token"}]`.
- **`servers`** — the git-host credentials, `[{"host","kind","token","user?"}]`
  (host-selected; `kind` picks the HTTPS basic-auth username: github →
  `x-access-token`, gitlab → `oauth2`, gitea → the real account).

plus `require-auth` (boolean).

## Tools

`git-clone`, `git-status`, `git-log`, `git-diff`, `git-branch`, `git-checkout`,
`git-add`, `git-commit`, `git-pull`, `git-push`, `git-show`.

## Wire contract

The worker.v1 proto is **vendored** under `proto/worker/v1/` (copied from
easyworker) and the Go client stubs are generated with `buf` (connect-go v2).

## Build / test

```bash
go build ./cmd/gitea-ext
go test ./...
```

Tests use an in-process fake worker (no real sandbox needed).
