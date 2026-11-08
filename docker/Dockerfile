# syntax=docker/dockerfile:1
# Copyright (c) 2026 karamble
# Use of this source code is governed by an ISC
# license that can be found in the LICENSE file.
#
# goarmin's server (API, database, dashboard) as a container image. The build context is the
# repository root:
#
#   docker compose -f docker/compose.yaml build      (see docker/README.md)
#   docker build -f docker/Dockerfile -t goarmin:local .
#
# The sync role (Bluetooth to the watch) stays on a host with the watch's pairing; this image
# runs `goarmin serve` without it.

# 1. The dashboard: React, built by Vite into internal/webui/dist.
FROM node:26.13.0-alpine3.24 AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json web/.npmrc ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# 2. The binary: static (no cgo), time zones embedded, the dashboard embedded.
FROM golang:1.27.3-alpine3.24 AS go
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS=-mod=readonly
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY pkg ./pkg
COPY --from=web /src/internal/webui/dist ./internal/webui/dist
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -tags timetzdata -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/goarmin ./cmd/goarmin

# 3. The runtime: Alpine with the time zone database (day boundaries and the watch's clock use
# each owner's IANA zone; also embedded in the binary) and GNU time (/usr/bin/time, for timing
# imports: docker compose exec goarmin time -v goarmin import …).
FROM alpine:3.24.4
RUN apk add --no-cache tzdata time \
 && adduser -D -H -u 10001 -s /sbin/nologin goarmin \
 && mkdir -p /data /backup \
 && chown 10001:10001 /data /backup \
 && chmod 700 /data /backup
COPY --from=go /out/goarmin /usr/local/bin/goarmin
ENV GOARMIN_DATA_DIR=/data \
    GOARMIN_BACKUP_DIR=/backup \
    GOARMIN_LISTEN=0.0.0.0:8642
USER 10001:10001
EXPOSE 8642
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
  CMD ["goarmin", "healthcheck", "-q"]
ENTRYPOINT ["goarmin"]
CMD ["serve", "-no-usb"]
