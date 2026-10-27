# syntax=docker/dockerfile:1
# A static Go binary. go-git is pure Go, so no system git is needed, and the runtime image is a
# bare alpine base.
#
# PORTABILITY: nothing environment-specific lives here. No internal mirror, no proxy, no
# *.svc.cluster.local. A CI runner and a laptop build the same image; anything a particular
# environment needs is supplied as a build-arg or by the container runtime.

# The browser UI, built FIRST and copied into the Go tree before `go build`.
#
# It is a build stage rather than committed output: a stale dist/ in the repository would be served
# in place of the real one, and the failure looks like "the UI is wrong" rather than "the build is
# wrong". Keeping the toolchain here also means a contributor needs nothing but Docker.
FROM node:26-alpine AS ui
WORKDIR /ui
# The lockfile is committed (see .gitignore: node_modules is not, the lock is). `npm ci` requires it,
# so the image is reproducible and a drift between package.json and the lock fails here instead of
# resolving something new at build time.
COPY ui/package.json ui/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY ui/ ./
RUN npm run build
# Fail the BUILD, not the deploy, if the bundle is empty.
#
# go:embed of an empty directory is legal, so a UI stage that produced nothing yields a binary with
# no assets and an extension that 404s every UI path — with a green CI. That is exactly what
# happened: the `ui` job passed (it builds the bundle separately) while the image shipped without it.
# Checking here makes the failure land where the mistake is.
RUN test -f /ui/dist/index.html || (echo "FATAL: vite produced no index.html" >&2; ls -la /ui; exit 1)

FROM golang:1.26-alpine AS build
WORKDIR /build
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
# go:embed cannot reach outside the package directory, so the built UI is placed where
# cmd/github-ext/ui declares it. See the //go:embed directive in main.go.
COPY --from=ui /ui/dist ./cmd/github-ext/ui
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/github-ext ./cmd/github-ext

FROM alpine:3.24
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build /out/github-ext /app/github-ext

# Workspaces are git checkouts on this pod's LOCAL disk. No database, no volume:
#
#   - a checkout is a session's private scratchpad, so a shared volume would let two pods fight
#     over one index;
#   - losing them on restart is acceptable and arguably correct — a stale checkout of a branch
#     that has moved is worse than no checkout, because it looks usable.
#
# WORKSPACE_IDLE_TTL means an abandoned checkout does not accumulate until the disk fills.
ENV WORKSPACE_ROOT=/tmp/workspaces
ENV WORKSPACE_IDLE_TTL=2h
ENTRYPOINT ["/app/github-ext"]
