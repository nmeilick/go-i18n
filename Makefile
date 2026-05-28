.DEFAULT_GOAL := _default

APP ?= lingo
MODULE ?= $(shell go list -m)
CMD ?= ./cmd/lingo
BIN_DIR ?= bin
BIN ?= $(BIN_DIR)/$(APP)
DIST_DIR ?= dist
VERSION ?= dev
SNAPSHOT ?= 0
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || printf unknown)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
CGO_ENABLED ?= 0
GO ?= go
GOFLAGS ?= -trimpath
STATICCHECK ?= staticcheck
VULN_TOOL ?= govulncheck
BUILDINFO_PKG ?= $(MODULE)/cmd/lingo/internal/cli
LDFLAGS ?= -s -w \
	-X $(BUILDINFO_PKG).Version=$(VERSION) \
	-X $(BUILDINFO_PKG).Commit=$(COMMIT) \
	-X $(BUILDINFO_PKG).Date=$(DATE)
PLATFORMS ?= linux/amd64 linux/arm64 windows/amd64 darwin/amd64 darwin/arm64
CHECK_TARGETS ?= fmt-check lint test catalog-check data-check smoke
RELEASE_TARGETS ?= _guard-release-version check vuln dist checksums
MAKE_SELF ?= $(firstword $(MAKEFILE_LIST))
MAKE_RECURSE = $(MAKE) --no-print-directory -f "$(MAKE_SELF)"

.PHONY: _default help build run test race vet lint fmt fmt-check format vuln govulncheck smoke catalog-check data-check check dist checksums release clean _guard-bin-dir _guard-dist-dir _guard-release-version

_default:
	@printf 'hint: run `make help` to list available targets\n'
	@$(MAKE_RECURSE) build

help: ## Show available targets.
	@awk 'BEGIN {FS = ":.*## "; printf "Targets:\n"} /^[a-zA-Z0-9_.-]+:.*## / {printf "  %-12s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: _guard-bin-dir ## Build bin/lingo.
	@mkdir -p "$(BIN_DIR)"
	CGO_ENABLED="$(CGO_ENABLED)" $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o "$(BIN)" "$(CMD)"

run: ## Run lingo locally; pass arguments with ARGS="...".
	$(GO) run $(GOFLAGS) "$(CMD)" $(ARGS)

test: ## Run Go tests.
	$(GO) test ./...

race: ## Run race-enabled Go tests.
	$(GO) test -race ./...

vet: ## Run go vet.
	$(GO) vet ./...

lint: ## Run go vet and staticcheck.
	@$(MAKE_RECURSE) vet
	@command -v "$(STATICCHECK)" >/dev/null 2>&1 || \
		{ printf 'error: %s not found; install honnef.co/go/tools/cmd/staticcheck\n' "$(STATICCHECK)" >&2; exit 2; }
	$(STATICCHECK) ./...

fmt: ## Format Go source files.
	gofmt -w $$(find . -path ./.git -prune -o -name '*.go' -print)

fmt-check: ## Check Go formatting without modifying files.
	@files=$$(gofmt -l $$(find . -path ./.git -prune -o -name '*.go' -print)); \
	if [ -n "$$files" ]; then \
		printf 'error: gofmt needed for:\n%s\n' "$$files" >&2; \
		exit 1; \
	fi

vuln: ## Run Go vulnerability checks.
	@command -v "$(VULN_TOOL)" >/dev/null 2>&1 || \
		{ printf 'error: %s not found; install golang.org/x/vuln/cmd/govulncheck\n' "$(VULN_TOOL)" >&2; exit 2; }
	$(VULN_TOOL) ./...

smoke: ## Build and run a minimal CLI version smoke check.
	@$(MAKE_RECURSE) build
	"$(BIN)" version >/dev/null

catalog-check: ## Check the showcase catalog workflow strictly.
	@$(MAKE_RECURSE) build
	"$(BIN)" --config examples/showcase/lingo.toml check --strict

data-check: ## Check generated CLDR data against the local lock and assets.
	@$(MAKE_RECURSE) build
	"$(BIN)" data check

check: ## Run normal pre-handoff verification.
	@$(MAKE_RECURSE) $(CHECK_TARGETS)

dist: _guard-dist-dir ## Build multi-platform artifacts into dist/.
	@rm -rf "$(DIST_DIR)"
	@mkdir -p "$(DIST_DIR)"
	@set -e; \
	for platform in $(PLATFORMS); do \
		goos=$${platform%/*}; \
		goarch=$${platform#*/}; \
		if [ "$$goos" = "$$platform" ] || [ -z "$$goos" ] || [ -z "$$goarch" ]; then \
			printf 'error: invalid platform %s; use GOOS/GOARCH\n' "$$platform" >&2; \
			exit 2; \
		fi; \
		ext=; \
		if [ "$$goos" = windows ]; then ext=.exe; fi; \
		out="$(DIST_DIR)/$(APP)-$$goos-$$goarch$$ext"; \
		printf 'building %s\n' "$$out"; \
		GOOS="$$goos" GOARCH="$$goarch" CGO_ENABLED="$(CGO_ENABLED)" \
			$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o "$$out" "$(CMD)"; \
	done

checksums: _guard-dist-dir ## Write SHA-256 checksums for dist artifacts.
	@test -d "$(DIST_DIR)" || { printf 'error: %s does not exist; run make dist first\n' "$(DIST_DIR)" >&2; exit 2; }
	@rm -f "$(DIST_DIR)/checksums.txt"
	@if command -v sha256sum >/dev/null 2>&1; then \
		(cd "$(DIST_DIR)" && sha256sum * > checksums.txt); \
	elif command -v shasum >/dev/null 2>&1; then \
		(cd "$(DIST_DIR)" && shasum -a 256 * > checksums.txt); \
	else \
		printf 'error: sha256sum or shasum is required\n' >&2; \
		exit 2; \
	fi

release: ## Create a verified local release bundle; never publishes.
	@$(MAKE_RECURSE) $(RELEASE_TARGETS)

clean: _guard-bin-dir _guard-dist-dir ## Remove generated local artifacts.
	rm -rf "$(BIN_DIR)" "$(DIST_DIR)" coverage coverage.out coverage.html *.coverprofile *.test .lingo-cldr-* cldr-size-report.*

format: fmt

govulncheck: vuln

_guard-bin-dir:
	@case "$(BIN_DIR)" in ""|"/"|"."|".."|/*|../*|*/../*) \
		printf 'error: unsafe BIN_DIR=%s\n' "$(BIN_DIR)" >&2; exit 2;; \
	esac

_guard-dist-dir:
	@case "$(DIST_DIR)" in ""|"/"|"."|".."|/*|../*|*/../*) \
		printf 'error: unsafe DIST_DIR=%s\n' "$(DIST_DIR)" >&2; exit 2;; \
	esac

_guard-release-version:
	@if [ "$(VERSION)" = dev ] && [ "$(SNAPSHOT)" != 1 ]; then \
		printf 'error: set VERSION=<version> for release, or SNAPSHOT=1 for a local snapshot\n' >&2; \
		exit 2; \
	fi

.DEFAULT:
	@printf 'unknown target: %s\n\n' '$@' >&2
	@$(MAKE_RECURSE) help >&2
	@exit 2
