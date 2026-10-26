# Stream a build's output to a check run

GitHub does not expose a running job's log through any public API. It serves the log from an
internal store, and the web endpoints that the log viewer uses need a browser session cookie — a
PAT gets a 404. What *is* readable while a job runs is a check run's `output.text`, with a plain
`GET` and a PAT.

This action keeps that field up to date as your command prints, so something outside the runner can
follow the build live.

## Use

```yaml
jobs:
  build:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      checks: write        # required: the action creates and updates a check run
    steps:
      - uses: actions/checkout@v4
      - uses: easydev-org/easyagent-github-ext/actions/log-stream@main
        with:
          command: make test
```

The step's own log is unchanged — output goes to both places.

## Read it from outside

```
GET https://api.github.com/repos/{owner}/{repo}/check-runs/{id}
Authorization: Bearer <token>        # optional for a public repository

→ .output.text
```

The check run id appears in the step's log as `log-stream: check run <id>`. Since a job and its
check run share an id, the value from `GET /actions/runs/{run}/jobs` works here too.

## What it does not do

**It cannot publish more than 65535 characters.** That is GitHub's limit on `output.text`, and it
is not configurable. The buffer therefore keeps the newest lines and drops the oldest with a
counted note on the first line. A build that prints a megabyte has its early output dropped — the
end is what a watcher is waiting for, and the full log is still in the Actions log afterwards.

**It only helps repositories that run it.** Nothing else can make a third party's build stream: the
content of `output.text` is whatever that workflow chooses to put there.

## Cost

One `PATCH` per `interval` seconds, each carrying the whole buffer. At the default 5s and a full
65535-character buffer that is about 13 KB/s of upload from the runner, and 720 requests/hour
against the token's rate limit. Raise `interval` for long, chatty builds.
