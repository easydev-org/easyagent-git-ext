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
# easyvcs keeps repository OBJECTS in its database, so this path is the staged content itself —
# it must be a persistent volume. An emptyDir would erase every revision on a pod restart.
ENV EASYVCS_DRIVER=sqlite
ENV EASYVCS_DSN=/var/lib/easyvcs/easyvcs.db
VOLUME ["/var/lib/easyvcs"]
ENTRYPOINT ["/app/github-ext"]
