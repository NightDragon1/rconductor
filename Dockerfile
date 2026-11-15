# syntax=docker/dockerfile:1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e
# SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Images for panel and agent (targets "panel" and "agent"). Build stages run
# on the build platform and cross-compile, so amd64 and arm64 come from one
# buildx call. Both images are distroless static as non-root (Technology 4).

FROM --platform=$BUILDPLATFORM node:26-trixie-slim@sha256:e6bedef217677b276c591795e169840edd9096dbf13f29df8ef8d1856b8820bc AS ui
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
COPY internal/webui/dist/.gitkeep /src/internal/webui/dist/.gitkeep
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.27.4-trixie@sha256:b54b2639de8c1bfe538ec5ffc45f29b1932235ac5f6d63c059853646677112d9 AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY tools/go.mod tools/go.sum tools/
RUN go mod download \
 && go -C tools build -o /usr/local/bin/go-licenses github.com/google/go-licenses/v2
COPY . .
COPY --from=ui /src/internal/webui/dist/ internal/webui/dist/
ARG TARGETOS TARGETARCH
ARG VERSION=0.0.0-dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
RUN ldflags="-s -w -X github.com/NightDragon1/rconductor/internal/buildinfo.Version=$VERSION \
      -X github.com/NightDragon1/rconductor/internal/buildinfo.Commit=$COMMIT \
      -X github.com/NightDragon1/rconductor/internal/buildinfo.BuildDate=$BUILD_DATE" \
 && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "$ldflags" \
      -o /out/panel ./cmd/panel \
 && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "$ldflags" \
      -o /out/agent ./cmd/agent
# /licenses in each image (NFR-021): the license and the licenses of the
# linked Go modules, for the panel also those of the bundled UI.
RUN mkdir -p /out/licenses-panel /out/licenses-agent \
 && cp LICENSE /out/licenses-panel/LICENSE && cp LICENSE /out/licenses-agent/LICENSE \
 && { scripts/third-party-go.sh go-licenses ./cmd/panel \
      && echo && cat internal/webui/dist/third-party-licenses.txt; } > /out/licenses-panel/THIRD_PARTY \
 && scripts/third-party-go.sh go-licenses ./cmd/agent > /out/licenses-agent/THIRD_PARTY
# Volume mount points owned by the non-root user, so named volumes start
# writable; /secrets is private to that user.
RUN mkdir -p /out/panel-root/data /out/panel-root/backups /out/agent-root \
 && install -d -m 0700 /out/panel-root/secrets /out/agent-root/secrets

FROM gcr.io/distroless/static-debian13:nonroot@sha256:aafe993665bf6200db38a02c11a9351e782a84b6a45e7f33b6695638c88e3931 AS panel
ARG VERSION=0.0.0-dev
ARG COMMIT=unknown
LABEL org.opencontainers.image.title="RCONductor panel" \
      org.opencontainers.image.source="https://github.com/NightDragon1/rconductor" \
      org.opencontainers.image.licenses="AGPL-3.0-or-later" \
      org.opencontainers.image.version="$VERSION" \
      org.opencontainers.image.revision="$COMMIT" \
      org.opencontainers.image.authors="NightDragon (ShadowGhost.NET)"
COPY --from=build /out/licenses-panel/ /licenses/
COPY --from=build --chown=65532:65532 /out/panel-root/ /
COPY --from=build /out/panel /usr/local/bin/rconductor-panel
USER 65532:65532
# Web port and agent port (D-032).
EXPOSE 8080 8443
VOLUME ["/data", "/secrets", "/backups"]
ENTRYPOINT ["/usr/local/bin/rconductor-panel"]

FROM gcr.io/distroless/static-debian13:nonroot@sha256:aafe993665bf6200db38a02c11a9351e782a84b6a45e7f33b6695638c88e3931 AS agent
ARG VERSION=0.0.0-dev
ARG COMMIT=unknown
LABEL org.opencontainers.image.title="RCONductor agent" \
      org.opencontainers.image.source="https://github.com/NightDragon1/rconductor" \
      org.opencontainers.image.licenses="AGPL-3.0-or-later" \
      org.opencontainers.image.version="$VERSION" \
      org.opencontainers.image.revision="$COMMIT" \
      org.opencontainers.image.authors="NightDragon (ShadowGhost.NET)"
COPY --from=build /out/licenses-agent/ /licenses/
COPY --from=build --chown=65532:65532 /out/agent-root/ /
COPY --from=build /out/agent /usr/local/bin/rconductor-agent
# Non-root; the Docker socket is reached through its group (group_add with
# the socket's GID) or a socket proxy (revision 2.3 candidate 7). The agent
# opens no port (AGT-011).
USER 65532:65532
VOLUME ["/secrets"]
ENTRYPOINT ["/usr/local/bin/rconductor-agent"]
