# syntax=docker/dockerfile:1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Images for panel and agent (targets "panel" and "agent"). Build stages run
# on the build platform and cross-compile, so amd64 and arm64 come from one
# buildx call. Both images are distroless static as non-root (Technology 4).

FROM --platform=$BUILDPLATFORM node:26-trixie-slim@sha256:e4db4c914bf8ad241bd24804fc25d272d89bad97e68cdc8a1414862790a02c39 AS ui
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
COPY internal/webui/dist/.gitkeep /src/internal/webui/dist/.gitkeep
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.27.3-trixie@sha256:c65ecfe613b539ac74c62a4c3b2c27e8f8a6076384c8f27e8a3c0f3be17cc385 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui /src/internal/webui/dist/ internal/webui/dist/
ARG TARGETOS TARGETARCH
ARG VERSION=0.0.0-dev
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
      -ldflags "-s -w -X github.com/NightDragon1/rconductor/internal/buildinfo.Version=$VERSION" \
      -o /out/panel ./cmd/panel \
 && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
      -ldflags "-s -w -X github.com/NightDragon1/rconductor/internal/buildinfo.Version=$VERSION" \
      -o /out/agent ./cmd/agent
# Volume mount points owned by the non-root user, so named volumes start
# writable; /secrets is private to that user.
RUN mkdir -p /out/panel-root/data /out/panel-root/backups /out/agent-root \
 && install -d -m 0700 /out/panel-root/secrets /out/agent-root/secrets

FROM gcr.io/distroless/static-debian13:nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329 AS panel
ARG VERSION=0.0.0-dev
LABEL org.opencontainers.image.title="RCONductor panel" \
      org.opencontainers.image.source="https://github.com/NightDragon1/rconductor" \
      org.opencontainers.image.licenses="AGPL-3.0-or-later" \
      org.opencontainers.image.version="$VERSION"
COPY --from=build --chown=65532:65532 /out/panel-root/ /
COPY --from=build /out/panel /usr/local/bin/rconductor-panel
USER 65532:65532
# Web port and agent port (D-032); the agent port follows in M3.
EXPOSE 8080 8443
VOLUME ["/data", "/secrets", "/backups"]
ENTRYPOINT ["/usr/local/bin/rconductor-panel"]

FROM gcr.io/distroless/static-debian13:nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329 AS agent
ARG VERSION=0.0.0-dev
LABEL org.opencontainers.image.title="RCONductor agent" \
      org.opencontainers.image.source="https://github.com/NightDragon1/rconductor" \
      org.opencontainers.image.licenses="AGPL-3.0-or-later" \
      org.opencontainers.image.version="$VERSION"
COPY --from=build --chown=65532:65532 /out/agent-root/ /
COPY --from=build /out/agent /usr/local/bin/rconductor-agent
# Stub until M3; access to the Docker socket (root or docker group) is
# decided there.
USER 65532:65532
VOLUME ["/secrets"]
ENTRYPOINT ["/usr/local/bin/rconductor-agent"]
