#!/usr/bin/env bash
# Run the gitea-ext live e2e against a sandbox worker that is ALREADY CLAIMED
# (WORKER_TOKEN set), per the platform contract. Does NOT start a worker.
#
#   NATS_URL=nats://127.0.0.1:4222 WORKER_URL=http://127.0.0.1:48080 \
#     WORKER_TOKEN=... ./interop/live/run.sh
set -euo pipefail
NATS_URL="${NATS_URL:-nats://127.0.0.1:4222}"
WORKER_URL="${WORKER_URL:?set WORKER_URL (the claimed worker)}"
WORKER_TOKEN="${WORKER_TOKEN:?set WORKER_TOKEN}"
REMOTE="${REMOTE:-https://github.com/octocat/Hello-World.git}"

go build -buildvcs=false -o /tmp/gitea-ext-e2e-live ./interop/live
/tmp/gitea-ext-e2e-live "$NATS_URL" "$WORKER_URL" "$WORKER_TOKEN" "$REMOTE"
