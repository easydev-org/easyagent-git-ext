# AGENTS.md — easyagent-git-ext

Working notes for agents editing this repo.

This extension talks to GitHub: workspaces (git clones), issues, pull requests, releases and
artifacts. Workspaces are plain directories on the pod's **local** disk — a checkout is a session's
private scratchpad, and two pods sharing one would fight over the index.

**End-to-end testing follows the shared convention in
[`easybus-sdk/E2E.md`](https://github.com/easydev-org/easybus-sdk/blob/main/E2E.md).** Read that
first; this file only records what is specific to this repo.

---

## The two test suites

| job | services | proves |
|---|---|---|
| `unit` | none | build, vet, `-race` tests |
| `live` | nats only | a **real clone** of `easydev-org/test`, and an impossible patch is **refused** |

## Why the repository is real

The tool under test **is git**. A fixture would prove nothing about credentials, transport or ref
handling — which is exactly where this extension fails. So the suite clones
`https://github.com/easydev-org/test` for real.

It is read-only from the test's side: shallow clone, list, and assert a patch that cannot apply is
**refused** rather than half-applied. Nothing is pushed.

Needs `secrets.E2E_GITHUB_TOKEN` (read access to that repository). The job is skipped on
`pull_request` because a fork cannot read the secret — a suite that silently skips is worse than one
that does not run, so check the log when it does not appear.

## Traps in this repo specifically

### The config write is asynchronous

`token` goes into the `cfg` KV under `t.<tenant>.github.token`, and the extension picks it up from
its watch. A clone issued immediately after the write races that watch and fails as though the token
were absent.

### `Deps.ReportProgress` is nullable, and nil means "disabled"

Pass `nil`, not a no-op stub. A stub returning nil pretends a progress report was published; `nil`
is the documented way to say there is no channel to publish on. The signature takes
`*easybus.ToolProgress`, not `any`.

### Tools are permission-gated

`tools.RequireMap()` attaches each tool's `requires:` to its spec, so a call without grants is
refused before any work. The suite passes what an operator would issue via `CallToolWithGrants`.
