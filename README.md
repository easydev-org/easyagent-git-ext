# easyagent-git-ext

A standalone easyagent **extension server** that serves git operations —
`clone` / `status` / `log` / `diff` / `branch` / `checkout` / `add` / `commit` /
`pull` / `push` / `show` — against **any git host** (github.com, gitlab.com,
gitea, or a self-hosted instance), over the easybus protocol.

Pure Go, using **go-git** — the runtime image needs **no system `git`**.

## Why it is forge-agnostic

The git **transport** is standard (smart HTTP / SSH), so clone/push/pull work
identically on every host. A host's `kind` only selects the **HTTPS basic-auth
username convention**:

| `kind` | HTTPS auth username |
|---|---|
| `github` | `x-access-token` |
| `gitlab` | `oauth2` |
| `gitea` | the real account (or `user` override) |
| `generic` | `token` |

(Forge *API* operations — pull requests, issues — are deliberately **not** here:
those differ per platform and would need a per-forge adapter. This extension is
the transport layer, which is uniform.)

## Configuration

Two knobs, both edited from the agent's Extensions page:

```yaml
config:
  - name: servers          # SECRET: never returned to a client
    type: string
    kind: secret
  - name: default-branch
    type: string
    default: main
  - name: require-auth
    type: boolean
    default: false
```

`servers` is a JSON list, one entry per host:

```json
[
  {"host": "github.com",      "kind": "github", "token": "ghp_..."},
  {"host": "gitlab.corp.com", "kind": "gitlab", "token": "glpat-...", "user": "oauth2"},
  {"host": "gitea.example.com","kind": "gitea",  "token": "gt_...", "user": "alice"}
]
```

- A server is selected by the **remote URL's host** (parsed from https://,
  ssh://, or `git@host:path`).
- `token` empty ⇒ anonymous (public repos). `require-auth: true` makes an
  unconfigured host an error instead of falling back to anonymous.
- `kind: secret` means the **agent withholds the value** from every client: the
  UI shows only "set / not set", never the token.

## Design

- **Own process, own repo.** The agent discovers it over `easybus.discover`
  plus an `easybus-presence` heartbeat.
- **Per-tenant repo cache.** Each (tenant, remote) gets its own clone under
  `GITEXT_HOME` (`/data/repos` by default). The tenant is a sanitized path
  segment and the remote is hashed, so one tenant can never reach another's
  files. A per-repo lock serialises concurrent tool calls.
- **Clone-if-missing.** `git-clone` (and every other tool) opens the cached
  clone, cloning it on first use — so a tool sequence is just a series of calls
  with the same `remote`.
- **Credentials never touch the wire beyond the agent.** Tokens ride the
  (secret) config; the tools resolve the right one by host.

## Tools

`git-clone`, `git-status`, `git-log`, `git-diff`, `git-branch`, `git-checkout`,
`git-add`, `git-commit`, `git-pull`, `git-push`, `git-show`.

Every tool takes a `remote` (the git URL). The first call clones it; later calls
reuse the clone.

## Run

```sh
NATS_URL=nats://nats:4222 GITEXT_HOME=/data/repos ./git-ext
```

## Build

```bash
go build ./cmd/git-ext
./build-image.sh            # buildkitd -> registry OCI
```

## Test

```bash
go test ./...
```

Tests use a local bare repo (no network); the system `git` is used **only** in
the test helper to seed the remote — the extension itself never needs it.
