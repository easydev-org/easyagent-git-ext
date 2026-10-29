# easyagent-gitdev-ext

One extension for the job of changing a repository and landing the change: a **mirror** to edit and
**GitHub** to review, merge and release. No clone, no local git, no workspace directory.

## The shape of it

GitHub is the authority. The mirror is a writable copy of the repository that changes are made in,
through the forge's own API — a branch is a ref, a file is an object, and a commit is one call.

```
git-sync-upstream    make main current         (fast-forward only; refuses and names both shas)
git-branch-create    a working branch          (from a local branch, or from one only the upstream has)
git-file-read        read a file               (line-numbered)
git-file-patch       change it                 (ONE patch, ONE commit — the only writer)
git-file-list        list a directory
git-push-branch      send that branch          (and nothing else)
        ↓
gh-pr-create         open the request
gh-pr-checks         CI and mergeability
gh-pr-merge          land it
        ↓
git-sync-upstream    next task starts from the merged state
```

## Why there is no workspace

There used to be one, plus a staging area backed by an `easyvcs` database. Both existed to avoid a
clone. The forge's API removes the reason: a branch and a file are objects it can read and write
directly, which is cheaper than a checkout and cannot disagree with the authoritative history.

The one thing a checkout could do that this cannot is run a build. That was never offered here —
there is no `exec` anywhere in this extension — so nothing was lost.

## The panel

Mounted at `/ext/gitdev/` and reachable from the host's extension shelf. Six routes, all deep-linkable:

| Route | What it shows |
|---|---|
| `#/` | **the door**: repositories you can see, with organisations as tabs |
| `#/files/<owner>/<name>?branch=&dir=` | the mirror's files: tree, file contents, branch switch |
| `#/pulls/<owner>/<name>` | open pull requests |
| `#/pull/<owner>/<name>/<n>` | one pull request: files, diff, CI |
| `#/issues/<owner>/<name>` | open issues |
| `#/issue/<owner>/<name>/<n>` | one issue: body and conversation |
| `#/runs/<owner>/<name>` | recent CI runs |
| `#/run/<owner>/<name>/<id>` | one run: jobs, steps, and **logs on demand** |
| `#/releases/<owner>/<name>` | releases, with their notes |
| `#/workflows/<owner>/<name>` | the workflow DEFINITIONS, not their runs |
| `#/card/<template>` | one tool result, embedded in a conversation |

A tool result carries a `view`, so a card offers a button that opens the page for the thing the
result was about. The route is built by the tool, because only it knows which repository the result
came from.

### Two levels of navigation

```
#/                       repositories            <- where a reader starts
#/files/o/n  ...         one repository          <- the six tabs
```

The bar names the repository and offers Files, Pulls, Issues, CI, Releases, Workflows, plus a `<-`
back to the list. **The back arrow is not decoration**: every page below it names a repository, so
without it choosing one was a one-way trip.

**A page error renders INSIDE the frame.** It used to replace the whole page, which took the bar with
it — a reader whose repository failed to load was left on a line of text with no way back. A failure
of the content must not remove the navigation.

**Headers do not shrink.** Every page here is a column whose other child is a scrolling list, and a
flex child shrinks by default — so a header without `flex: 0 0 auto` gets squashed as the list takes
the space, and its text clips instead of the layout scrolling. All eight page headers say it.

### Decisions worth knowing before changing it

**Logs are fetched per job, on demand.** A run's logs are megabytes; a page that prefetched them
would make opening it cost everything whether or not anybody scrolled that far. The step list answers
"where is it" in a few hundred bytes.

**Nothing in the panel writes.** Browsing and reviewing are reads; every change goes through a tool,
where the permission model already lives. A page that could merge would need its own permission story
and does not have one.

**A job that has not finished has no log** — GitHub publishes it on completion. The page offers no
button for one and says why, rather than a button that fails.

**The two CI pages poll** while something is running and stop when it finishes. This is the only
place in the panel where content changes on its own.

**The panel reads its token from `localStorage['agent.token']`**, which is the host's own key. The
UI is served from the host's origin, so the read is same-origin and no bridge is needed.

## Running it

```sh
go build ./...
go test ./...
cd ui && npm ci && npm run build   # produces ui/dist, which is go:embed-ed
```

`ui/dist` is built by the Dockerfile's first stage and never committed: a stale committed bundle is
served in place of the real one and looks like "the UI is wrong" rather than "the build is wrong".

### Configuration

| Knob | What it is |
|---|---|
| `servers` | the mirror hosts: `host`, `kind` (gitea/github/gitlab), `api_url`, `token` |
| `token` | the GitHub token |
| `watch-repos` | repositories whose releases are published on the `gitdev` channel |

### Live tests

The suite in `internal/forge` and `internal/ghclient` runs against real hosts when pointed at them:

```sh
FORGE_LIVE_URL=http://forgejo.example FORGE_LIVE_TOKEN=… WALK_REPO=owner/repo \
UPSTREAM_LIVE_URL=https://github.com/owner/repo.git UPSTREAM_LIVE_TOKEN=… \
go test ./internal/forge -run Live -v
```

`TestWalkthroughLive` is the one to run first: it makes a change end to end and checks each step
against what the next one needs, rather than asserting that each call returned no error.
