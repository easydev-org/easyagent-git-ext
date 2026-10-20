# syntax=docker/dockerfile:1
# A static Go binary — no system `git` needed (go-git is pure Go), so the runtime
# image is a bare alpine base.
#
# PORTABILITY: this file carries NOTHING environment-specific. No internal
# artifact mirror, no proxy, no regional package mirror, no *.svc.cluster.local.
# A CI runner and a developer laptop build the same image; anything a particular
# environment needs (a proxy, a corporate mirror) is supplied as a build-arg or
# by the container runtime, never baked in here.
FROM golang:1.26-alpine AS build
WORKDIR /build
COPY go.mod go.sum ./
# The cache mount is shared with the build below, so a source-only change does not
# re-download the module graph.
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/gitea-ext ./cmd/gitea-ext

FROM alpine:3.24
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build /out/gitea-ext /app/gitea-ext
# A writable home for the per-tenant repo cache (override with GITEXT_HOME).
ENV GITEXT_HOME=/data/repos
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/app/gitea-ext"]
