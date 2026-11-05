# SPDX-License-Identifier: AGPL-3.0-or-later

# A release tag (v1.0); without one a semantic dev version, so that panel
# and agent of one build share the major version (AGT-013).
VERSION ?= $(shell git describe --tags --dirty --match 'v[0-9]*' 2>/dev/null || echo 0.0.0-dev+$$(git rev-parse --short HEAD 2>/dev/null || echo unknown))
LDFLAGS := -s -w -X github.com/NightDragon1/rconductor/internal/buildinfo.Version=$(VERSION)
GOFLAGS := -trimpath
SQLC_VERSION := 1.31.1
SQLC ?= sqlc
export CGO_ENABLED := 0

.PHONY: all build web panel agent images test test-integration integration-tests test-minecraft test-e2e lint vuln generate check

all: lint test web build

build: panel agent

# The UI build lands in internal/webui/dist and is embedded into the panel.
web:
	cd web && npm ci && npm run build

panel:
	go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/panel ./cmd/panel

agent:
	go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/agent ./cmd/agent

test:
	go test ./...

# Local images for the integration tests; CI builds multi-arch with buildx.
PANEL_IMAGE ?= rconductor-panel:dev
AGENT_IMAGE ?= rconductor-agent:dev

images:
	docker buildx build --load --target panel --build-arg VERSION=$(VERSION) -t $(PANEL_IMAGE) .
	docker buildx build --load --target agent --build-arg VERSION=$(VERSION) -t $(AGENT_IMAGE) .

test-integration: images integration-tests

# The tests alone, for images that are already there (CI loads them).
integration-tests:
	RCD_PANEL_IMAGE=$(PANEL_IMAGE) go test -tags integration -count=1 ./...

# Walking skeleton against a real Minecraft server (pinned version, JAR
# cached in .cache/minecraft).
test-minecraft:
	jar=$$(scripts/minecraft-server.sh .cache/minecraft) && RCD_MINECRAFT_JAR=$$jar go test -tags minecraft -count=1 -run TestMinecraft ./internal/app/

# End-to-end tests of the web interface with Playwright against the e2e
# stack (internal/e2e/stack); builds the UI first. Only the browser is
# downloaded (into the user's cache); CI installs its system libraries.
test-e2e:
	cd web && npm ci --no-audit --no-fund && npm run build && npx playwright install chromium && npx playwright test

# Full pre-commit gate (also run by the hook in .githooks).
check:
	scripts/check.sh

lint:
	go tool -modfile=tools/go.mod golangci-lint run ./...

vuln:
	go tool -modfile=tools/go.mod govulncheck ./...

generate:
	$(SQLC) generate
	go generate ./...
