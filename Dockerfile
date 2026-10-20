# syntax=docker/dockerfile:1
# A static Go binary — no system `git` needed (go-git is pure Go), so the
# runtime image is a bare distroless/static base.
ARG REGISTRY=docker.io/library
FROM ${REGISTRY}/golang:1.26-alpine AS build
ARG HTTP_PROXY
ARG HTTPS_PROXY
ENV HTTP_PROXY=${HTTP_PROXY} \
    HTTPS_PROXY=${HTTPS_PROXY} \
    NO_PROXY=localhost,127.0.0.1,.svc.cluster.local,.svc \
    GOPROXY=https://goproxy.cn,direct \
    CGO_ENABLED=0
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -trimpath -ldflags "-s -w" -o /out/gitea-ext ./cmd/gitea-ext

FROM ${REGISTRY}/alpine:3.24
RUN sed -i 's|dl-cdn.alpinelinux.org|mirrors.aliyun.com|g' /etc/apk/repositories \
    && apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build /out/gitea-ext /app/gitea-ext
# A writable home for the per-tenant repo cache (override with GITEXT_HOME).
ENV GITEXT_HOME=/data/repos
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/app/gitea-ext"]
