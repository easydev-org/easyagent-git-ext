# syntax=docker/dockerfile:1
# A static Go binary. go-git is pure Go, so no system git is needed, and the runtime image is a
# bare alpine base.
#
# PORTABILITY: nothing environment-specific lives here. No internal mirror, no proxy, no
# *.svc.cluster.local. A CI runner and a laptop build the same image; anything a particular
# environment needs is supplied as a build-arg or by the container runtime.
FROM golang:1.26-alpine AS build
WORKDIR /build
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
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
