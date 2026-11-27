# Makefile for mail
# Go commands run only inside the project toolchain container.

PROJECT_NAME := $(shell value=$$(sed -n 's/^project_name:[[:space:]]*//p' IDEA.md 2>/dev/null | head -n1 | tr -d '[:space:]'); if [ -z "$$value" ]; then remote=$$(git remote get-url origin 2>/dev/null); if [ -n "$$remote" ]; then remote=$$remote; remote=$${remote%.git}; value=$${remote##*/}; else value=$$(basename "$$(pwd)"); fi; fi; printf '%s' "$$value")
PROJECT_ORG := $(shell value=$$(sed -n 's/^project_org:[[:space:]]*//p' IDEA.md 2>/dev/null | head -n1 | tr -d '[:space:]'); if [ -z "$$value" ]; then remote=$$(git remote get-url origin 2>/dev/null | tr ':' '/'); if [ -n "$$remote" ]; then remote=$${remote%.git}; value=$$(printf '%s\n' "$$remote" | sed -E 's|.*/([^/]+)/[^/]+$$|\1|'); else value=$$(basename "$$(dirname "$$(pwd)")"); fi; fi; printf '%s' "$$value")
INTERNAL_NAME := $(shell sed -n 's/^internal_name:[[:space:]]*//p' IDEA.md 2>/dev/null | head -n1 | tr -d '[:space:]')
ifeq ($(INTERNAL_NAME),)
INTERNAL_NAME := $(PROJECT_NAME)
endif

# Local version order: explicit user override, release.txt, then the short commit ID.
VERSION ?= $(shell v=; [ -r release.txt ] && v=$$(tr -d '[:space:]' < release.txt); if [ -n "$$v" ]; then echo "$$v"; else git rev-parse --short=7 HEAD 2>/dev/null || echo devel; fi)

BUILD_EPOCH := $(shell date -u +%s)
BUILD_DATE := $(shell date -u -d @$(BUILD_EPOCH) +"%Y-%m-%dT%H:%M:%SZ")
COMMIT_ID := $(shell git rev-parse --short=7 HEAD 2>/dev/null || echo N/A)
OFFICIAL_SITE := $(shell [ -f site.txt ] && cat site.txt || echo "${OFFICIAL_SITE:-}")

# Production builds are stripped; debug builds retain symbols and source lines.
LDFLAGS := -s -w \
	-X 'main.Version=$(VERSION)' \
	-X 'main.CommitID=$(COMMIT_ID)' \
	-X 'main.BuildEpoch=$(BUILD_EPOCH)' \
	-X 'main.internalName=$(INTERNAL_NAME)' \
	-X 'main.OfficialSite=$(OFFICIAL_SITE)'
DEBUG_LDFLAGS := \
	-X 'main.Version=$(VERSION)' \
	-X 'main.CommitID=$(COMMIT_ID)' \
	-X 'main.BuildEpoch=$(BUILD_EPOCH)' \
	-X 'main.internalName=$(INTERNAL_NAME)' \
	-X 'main.OfficialSite=$(OFFICIAL_SITE)'
DEBUG_GCFLAGS := all=-N -l

BINDIR ?= binaries
RELDIR ?= releases
GO_CACHE ?= $(HOME)/go/pkg/mod
GO_BUILD ?= $(HOME)/.cache/go-build/$(PROJECT_NAME)
DOCKER_MEM ?= 4g
DOCKER_CPUS ?= 2
DOCKER_CPU_SHARES ?= 256
DOCKER_TIMEOUT ?= 3600
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64 freebsd/amd64 freebsd/arm64
REGISTRY ?= ghcr.io/$(PROJECT_ORG)/$(PROJECT_NAME)

# Absolute output directories need matching mounts inside the build container.
DOCKER_VOLUMES := $(if $(filter /%,$(BINDIR)),-v $(BINDIR):$(BINDIR)) $(if $(filter /%,$(RELDIR)),-v $(RELDIR):$(RELDIR))
GO_DOCKER_RUN = timeout -k 30 $(DOCKER_TIMEOUT) docker run --rm --no-healthcheck \
	--name $(PROJECT_NAME)-$$(head -c6 /dev/urandom | od -An -tx1 | tr -d ' \n' | cut -c1-8) \
	$(DOCKER_VOLUMES) \
	--memory=$(DOCKER_MEM) --cpus=$(DOCKER_CPUS) --cpu-shares=$(DOCKER_CPU_SHARES) \
	-v $(PWD):/app \
	-v $(GO_BUILD):/usr/local/share/go/cache \
	-v $(GO_CACHE):/usr/local/share/go/pkg/mod \
	-w /app -e CGO_ENABLED=0 -e GOFLAGS=-buildvcs=false

.DEFAULT_GOAL := help
.PHONY: build test release docker clean help

# Debug binaries for the local platform and all supported distribution targets.
build: clean
	@mkdir -p $(BINDIR) $(GO_CACHE) $(GO_BUILD)
	@echo "Building debug binaries, version $(VERSION)..."
	@$(GO_DOCKER_RUN) -e BUILD_LDFLAGS="$(DEBUG_LDFLAGS)" -e BUILD_GCFLAGS="$(DEBUG_GCFLAGS)" casjaysdev/go:latest sh -ec ' \
		set -eu; \
		go mod tidy; \
		go mod download; \
		go generate ./src/graphql; \
		GOOS=$$(go env GOOS) GOARCH=$$(go env GOARCH) go build -buildvcs=false -gcflags "$$BUILD_GCFLAGS" -ldflags "$$BUILD_LDFLAGS" -o $(BINDIR)/$(PROJECT_NAME) ./src; \
		if [ -d src/client ]; then GOOS=$$(go env GOOS) GOARCH=$$(go env GOARCH) go build -buildvcs=false -gcflags "$$BUILD_GCFLAGS" -ldflags "$$BUILD_LDFLAGS" -o $(BINDIR)/$(PROJECT_NAME)-cli ./src/client; fi; \
		if [ -d src/agent ]; then GOOS=$$(go env GOOS) GOARCH=$$(go env GOARCH) go build -buildvcs=false -gcflags "$$BUILD_GCFLAGS" -ldflags "$$BUILD_LDFLAGS" -o $(BINDIR)/$(PROJECT_NAME)-agent ./src/agent; fi; \
		for platform in $(PLATFORMS); do \
			OS=$${platform%/*}; ARCH=$${platform#*/}; EXT=; \
			[ "$$OS" != windows ] || EXT=.exe; \
			GOOS=$$OS GOARCH=$$ARCH go build -buildvcs=false -gcflags "$$BUILD_GCFLAGS" -ldflags "$$BUILD_LDFLAGS" -o $(BINDIR)/$(PROJECT_NAME)-$$OS-$$ARCH$$EXT ./src; \
			if [ -d src/client ]; then GOOS=$$OS GOARCH=$$ARCH go build -buildvcs=false -gcflags "$$BUILD_GCFLAGS" -ldflags "$$BUILD_LDFLAGS" -o $(BINDIR)/$(PROJECT_NAME)-cli-$$OS-$$ARCH$$EXT ./src/client; fi; \
			if [ -d src/agent ]; then GOOS=$$OS GOARCH=$$ARCH go build -buildvcs=false -gcflags "$$BUILD_GCFLAGS" -ldflags "$$BUILD_LDFLAGS" -o $(BINDIR)/$(PROJECT_NAME)-agent-$$OS-$$ARCH$$EXT ./src/agent; fi; \
		done'
	@echo "Build complete: $(BINDIR)/"

# Run the same code-quality gates as CI, coverage-enforced tests and binary smoke checks.
test: build
	@mkdir -p $(GO_CACHE) $(GO_BUILD)
	@$(GO_DOCKER_RUN) casjaysdev/go:latest sh -ec ' \
		set -eu; \
		mkdir -p "$${TMPDIR:-/tmp}/$(PROJECT_ORG)"; \
		COVERAGE_DIR=$$(mktemp -d "$${TMPDIR:-/tmp}/$(PROJECT_ORG)/$(INTERNAL_NAME)-XXXXXX"); \
		trap "rm -rf \"$$COVERAGE_DIR\"" EXIT; \
		COVERAGE_DATA=$$COVERAGE_DIR/data; COVERAGE_FILE=$$COVERAGE_DIR/coverage.out; mkdir -p "$$COVERAGE_DATA"; \
		go mod download; go generate ./src/graphql; bash scripts/verify-licenses.sh; \
		go vet ./...; staticcheck ./...; govulncheck ./...; \
		shellcheck tests/*.sh; shfmt -d tests/*.sh; \
		TMPDIR="$$COVERAGE_DIR" go test -cover ./... -args -test.gocoverdir="$$COVERAGE_DATA"; \
		go tool covdata textfmt -i="$$COVERAGE_DATA" -o="$$COVERAGE_FILE"; \
		COVERAGE=$$(go tool cover -func="$$COVERAGE_FILE" | awk "/^total:/ {print \$$3}" | sed "s/%//"); \
		echo "Coverage: $$COVERAGE%"; \
		awk -v coverage="$$COVERAGE" "BEGIN {exit !(coverage + 0 >= 60)}"'
	@./tests/run_tests.sh

# Production binaries and checksums for local review; publication belongs to CI/CD.
release:
	@mkdir -p $(RELDIR) $(GO_CACHE) $(GO_BUILD)
	@find $(RELDIR) -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +
	@echo "Building production artifacts, version $(VERSION)..."
	@$(GO_DOCKER_RUN) -e BUILD_LDFLAGS="$(LDFLAGS)" casjaysdev/go:latest sh -ec ' \
		set -eu; go mod tidy; go mod download; go generate ./src/graphql; \
		for platform in $(PLATFORMS); do \
			OS=$${platform%/*}; ARCH=$${platform#*/}; EXT=; [ "$$OS" != windows ] || EXT=.exe; \
			GOOS=$$OS GOARCH=$$ARCH go build -buildvcs=false -trimpath -ldflags "$$BUILD_LDFLAGS" -o $(RELDIR)/$(PROJECT_NAME)-$$OS-$$ARCH$$EXT ./src; \
			if [ -d src/client ]; then GOOS=$$OS GOARCH=$$ARCH go build -buildvcs=false -trimpath -ldflags "$$BUILD_LDFLAGS" -o $(RELDIR)/$(PROJECT_NAME)-cli-$$OS-$$ARCH$$EXT ./src/client; fi; \
			if [ -d src/agent ]; then GOOS=$$OS GOARCH=$$ARCH go build -buildvcs=false -trimpath -ldflags "$$BUILD_LDFLAGS" -o $(RELDIR)/$(PROJECT_NAME)-agent-$$OS-$$ARCH$$EXT ./src/agent; fi; \
		done'
	@printf '%s\n' "$(VERSION)" > $(RELDIR)/version.txt
	@tar --exclude='.git' --exclude='.github' --exclude='.gitea' --exclude='binaries' --exclude='releases' --exclude='*.tar.gz' -czf $(RELDIR)/$(PROJECT_NAME)-$(VERSION)-source.tar.gz .
	@cd $(RELDIR) && files="$$(find . -maxdepth 1 -type f ! -name sha256.txt ! -name sha512.txt -printf '%f\n' | sort)" && sha256sum $$files > sha256.txt && sha512sum $$files > sha512.txt
	@echo "Production artifacts are ready in $(RELDIR)/; nothing was published."

# Build and load a host-platform image locally; CI/CD owns publishing.
docker:
	@docker buildx build --load -f docker/Dockerfile \
		--build-arg VERSION="$(VERSION)" \
		--build-arg BUILD_DATE="$(BUILD_DATE)" \
		--build-arg BUILD_EPOCH="$(BUILD_EPOCH)" \
		--build-arg COMMIT_ID="$(COMMIT_ID)" \
		--build-arg OFFICIAL_SITE="$(OFFICIAL_SITE)" \
		--tag $(REGISTRY):$(VERSION) --tag $(REGISTRY):latest .
	@echo "Built $(REGISTRY):$(VERSION) locally; no image was pushed."

clean:
	@rm -rf $(BINDIR) $(RELDIR)

help:
	@echo "Targets:"
	@echo "  build    Debug binaries for all platforms to $(BINDIR)/"
	@echo "  test     CI quality gates, coverage-enforced unit tests and binary smoke checks"
	@echo "  release  Production binaries, source archive and checksums to $(RELDIR)/ (no publish)"
	@echo "  docker   Host-platform image in the local Docker store (no push)"
	@echo "  clean    Remove $(BINDIR)/ and $(RELDIR)/"
	@echo "  help     This list"
