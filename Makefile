# SPDX-License-Identifier: AGPL-3.0-or-later

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.0.0-dev)
LDFLAGS := -s -w -X github.com/NightDragon1/rconductor/internal/buildinfo.Version=$(VERSION)
GOFLAGS := -trimpath
SQLC_VERSION := 1.31.1
SQLC ?= sqlc
export CGO_ENABLED := 0

.PHONY: all build web panel agent test test-integration lint vuln generate

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

test-integration:
	go test -tags integration -count=1 ./...

lint:
	go tool -modfile=tools/go.mod golangci-lint run ./...

vuln:
	go tool -modfile=tools/go.mod govulncheck ./...

generate:
	$(SQLC) generate
	go generate ./...
