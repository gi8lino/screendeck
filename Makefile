.DEFAULT_GOAL := help

## Tool Versions
# renovate: datasource=github-releases depName=golangci/golangci-lint
GOLANGCI_LINT_VERSION ?= v2.13.2

# renovate: datasource=github-releases depName=gi8lino/dev-tools
DEV_TOOLS_VERSION ?= v0.7.0

# renovate: datasource=github-releases depName=gi8lino/lore
LORE_VERSION ?= v0.16.0

## Shared development tools
include bin/dev-tools.mk
include $(call dev-tools-module,tag)
include $(call dev-tools-module,port)
include $(call dev-tools-module,browser)
include $(call dev-tools-module,help)

## Project-local tools
GOLANGCI_LINT := bin/golangci-lint
LORE := bin/lore
LORE_ASSET ?= lore_{version}_{os}_{arch}.tar.gz

## Documentation
DOCS_DIR := docs
SITE_CONFIG ?= $(DOCS_DIR)/site.toml
SITE_OUTPUT := $(DOCS_DIR)/site
SITE_PORT ?= $(call dev-port,site)
SITE_URL := http://127.0.0.1:$(SITE_PORT)/
PYTHON ?= python3

## Frontend
WEB_BUILD := scripts/web/build.sh
NODE ?= node

## Local smoke environment
SMOKE_COMPOSE := test/smoke/compose.yaml
SMOKE_ENV := test/smoke/.env
SMOKE_PROJECT := screendeck-smoke
SMOKE_MEDIA_GENERATE := scripts/dev/generate-media.sh
SMOKE_MEDIA_DIR := ./test/smoke/media/generated
SMOKE_DATA_DIR := /tmp/screendeck/data
SMOKE_ENV_ARG = $(if $(wildcard $(SMOKE_ENV)),--env-file $(SMOKE_ENV))
SMOKE = docker compose $(SMOKE_ENV_ARG) -p $(SMOKE_PROJECT) -f $(SMOKE_COMPOSE)
FFMPEG ?= ffmpeg

## Node.js Tooling
NPM ?= npm
NODE_MODULES := $(DOCS_DIR)/node_modules
NODE_BIN := $(NODE_MODULES)/.bin
NODE_DEPENDENCIES_STAMP := $(NODE_MODULES)/.screendeck-dependencies-installed

PRETTIER ?= $(NODE_BIN)/prettier
PLAYWRIGHT ?= $(NODE_BIN)/playwright

## Playwright Browser Cache
#
# Keep Playwright's browser binaries inside the repository instead of using
# the operating system's shared Playwright cache. The directory is ignored by
# Git and survives npm ci because it is outside node_modules.
PLAYWRIGHT_BROWSERS_PATH ?= $(DOCS_DIR)/.playwright
PLAYWRIGHT_BROWSER_STAMP := $(PLAYWRIGHT_BROWSERS_PATH)/.chromium-installed

IMAGE_CONVERT ?= magick

## Browser behavior tests
E2E_RUN := scripts/e2e/run.sh

## Documentation Screenshots
SCREENSHOT_MANIFEST := $(DOCS_DIR)/screenshots/screenshots.manifest
SCREENSHOT_RAW_DIR := $(DOCS_DIR)/screenshots/raw
SCREENSHOT_OUTPUT_DIR := $(DOCS_DIR)/content/assets/screenshots
SCREENSHOT_CAPTURE := scripts/screenshots/capture.sh
SCREENSHOT_NORMALIZE := scripts/screenshots/normalize.sh

## Formatting Sources
PRETTIER_MD_SOURCES := README.md "docs/content/**/*.md" "test/**/*.md"
PRETTIER_YAML_SOURCES := \
	".github/**/*.{yml,yaml}" \
	"deploy/**/*.{yml,yaml}" \
	"test/**/*.{yml,yaml}"
PRETTIER_JSON_SOURCES := ".github/**/*.json"

## Build Configuration
BINARY ?= screendeck
COMMAND ?= ./cmd
BUILD_VERSION ?= dev
BUILD_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS ?= -s -w -X main.Version=$(BUILD_VERSION) -X main.Commit=$(BUILD_COMMIT)

# Compatibility alias for the shared current target.
.PHONY: tag
tag: current

##@ Development

# Persistent application port; SQLite itself does not need a network port.
SCREENDECK_ASSIGNED_PORT ?= $(call dev-port,app)
SCREENDECK_URL := http://127.0.0.1:$(SCREENDECK_ASSIGNED_PORT)/
RUN_ARGS ?=

.PHONY: ports
ports: $(DEV_PORT) ## Print the saved local application port.
	@$(DEV_PORT) app --port "$(SCREENDECK_ASSIGNED_PORT)" > /dev/null
	@echo "ScreenDeck: $(SCREENDECK_URL)"

.PHONY: ports-reset
ports-reset: $(DEV_PORT) ## Clear saved ports after stopping local services.
	$(call run-tool,$(DEV_PORT),--reset)

.PHONY: dev-build
dev-build: ports
	$(MAKE) web

.PHONY: smoke-media
smoke-media: ## Generate deterministic synthetic media for local provider smoke tests.
	@FFMPEG="$(FFMPEG)" $(SMOKE_MEDIA_GENERATE) "$(SMOKE_MEDIA_DIR)"

.PHONY: smoke-media-clean
smoke-media-clean: ## Remove only the generated synthetic smoke media.
	rm -rf $(SMOKE_MEDIA_DIR)

.PHONY: smoke-up
smoke-up: ## Start the local ScreenDeck, Jellyfin, and Plex smoke stack.
	mkdir -p $(SMOKE_DATA_DIR)
	chmod 1777 $(SMOKE_DATA_DIR)
	$(SMOKE) up -d --build

.PHONY: smoke-down
smoke-down: ## Stop the local smoke stack while preserving its volumes.
	$(SMOKE) down --remove-orphans

.PHONY: smoke-reset
smoke-reset: ## Remove the local smoke stack and all provider/application state.
	$(SMOKE) down --volumes --remove-orphans

.PHONY: smoke-logs
smoke-logs: ## Follow logs from the local smoke stack.
	$(SMOKE) logs --follow screendeck jellyfin plex

.PHONY: web
web: ## Build the frontend distribution from web/src.
	@$(WEB_BUILD)

.PHONY: check-web
check-web: web ## Build the frontend and verify JavaScript parses.
	@find web/src/js -type f \( -name '*.js' -o -name '*.mjs' \) -exec $(NODE) --check {} \;

.PHONY: download
download: node-dependencies dev-tools ## Download Go, Node.js, and shared development dependencies.
	go mod download

.PHONY: run
run: dev-build $(OPEN_BROWSER) ## Build and run ScreenDeck using its saved port.
	@$(OPEN_BROWSER) "$(SCREENDECK_URL)" & \
	browser_pid=$$!; \
	trap 'kill "$$browser_pid" 2>/dev/null || true' EXIT; \
	$(MAKE) serve

.PHONY: serve
serve: ports ## Run ScreenDeck using its saved port (build must already be ready).
	go run $(COMMAND) \
		--listen-address="127.0.0.1:$(SCREENDECK_ASSIGNED_PORT)" \
		--base-url="$(patsubst %/,%,$(SCREENDECK_URL))" \
		--plex-url-override http://127.0.0.1:32400 \
		--debug --access-log --log-format text $(RUN_ARGS)

.PHONY: build
build: web ## Build the ScreenDeck binary.
	go build -ldflags="$(LDFLAGS)" -o $(BINARY) $(COMMAND)

.PHONY: vet
vet: web ## Run Go static analysis.
	go vet ./...

.PHONY: test
test: vet ## Run backend unit tests.
	go test -covermode=atomic -count=1 -parallel=4 -timeout=5m ./...

.PHONY: test-race
test-race: vet ## Run backend unit tests with the race detector.
	go test -race -count=1 -parallel=4 -timeout=5m ./...

.PHONY: test-e2e
test-e2e: web node-dependencies playwright-browser ## Run browser behavior tests against deterministic fixtures.
	@PLAYWRIGHT_BROWSERS_PATH="$(abspath $(PLAYWRIGHT_BROWSERS_PATH))" $(E2E_RUN)

.PHONY: cover
cover: web ## Display test coverage.
	go test -coverprofile=coverage.out -covermode=atomic -count=1 -parallel=4 -timeout=5m ./...
	go tool cover -html=coverage.out

.PHONY: clean
clean: ## Clean up generated application and documentation files.
	rm -f $(BINARY) coverage.out coverage.html
	rm -rf $(SITE_OUTPUT) web/dist

##@ Formatting

.PHONY: fmt
fmt: fmt-go fmt-md fmt-yaml fmt-json ## Format all supported files.

.PHONY: fmt-go
fmt-go: web ## Format Go code.
	go fmt ./...

.PHONY: fmt-md
fmt-md: node-dependencies ## Format Markdown files with Prettier.
	@$(PRETTIER) --write $(PRETTIER_MD_SOURCES)

.PHONY: fmt-yaml
fmt-yaml: node-dependencies ## Format YAML files with Prettier.
	@$(PRETTIER) --write $(PRETTIER_YAML_SOURCES)

.PHONY: fmt-json
fmt-json: node-dependencies ## Format JSON configuration files with Prettier.
	@$(PRETTIER) --write $(PRETTIER_JSON_SOURCES)

.PHONY: lint
lint: check-web lint-go lint-md lint-yaml lint-json ## Run all linters and formatting checks.

.PHONY: lint-go
lint-go: web golangci-lint ## Run golangci-lint.
	$(call run-tool,$(GOLANGCI_LINT),run)

.PHONY: lint-fix
lint-fix: web golangci-lint ## Run golangci-lint and apply fixes.
	$(call run-tool,$(GOLANGCI_LINT),run --fix)

.PHONY: lint-md
lint-md: node-dependencies ## Check Markdown formatting.
	@$(PRETTIER) --check $(PRETTIER_MD_SOURCES)

.PHONY: lint-yaml
lint-yaml: node-dependencies ## Check YAML formatting.
	@$(PRETTIER) --check $(PRETTIER_YAML_SOURCES)

.PHONY: lint-json
lint-json: node-dependencies ## Check JSON configuration files with Prettier.
	@$(PRETTIER) --check $(PRETTIER_JSON_SOURCES)

##@ Documentation

.PHONY: lore
lore: $(GITHUB_RELEASE_INSTALL) ## Install the pinned Lore static-site generator.
	@$(GITHUB_RELEASE_INSTALL) \
		--repo gi8lino/lore \
		--tag "$(LORE_VERSION)" \
		--asset "$(LORE_ASSET)" \
		--binary lore \
		--target "$(LORE)"

.PHONY: site
site: lore ## Build the documentation site with Lore.
	$(LORE) build --config "$(SITE_CONFIG)"

.PHONY: site-serve
site-serve: lore $(DEV_PORT) $(OPEN_BROWSER) ## Build, serve, and open the documentation site locally.
	$(LORE) build \
		--config "$(SITE_CONFIG)" \
		--site-url "$(SITE_URL)"
	@echo "Serving ScreenDeck documentation at $(SITE_URL)"
	@$(OPEN_BROWSER) "$(SITE_URL)" & \
	$(PYTHON) -m http.server $(SITE_PORT) --bind 127.0.0.1 --directory $(SITE_OUTPUT)

.PHONY: capture-screenshots
capture-screenshots: web playwright-browser ## Capture raw demo screenshots with Playwright.
	@PLAYWRIGHT="$(abspath $(PLAYWRIGHT))" \
	PLAYWRIGHT_BROWSERS_PATH="$(abspath $(PLAYWRIGHT_BROWSERS_PATH))" \
	SCREENSHOT_RAW_DIR="$(abspath $(SCREENSHOT_RAW_DIR))" \
	$(SCREENSHOT_CAPTURE)

.PHONY: screenshots
screenshots: capture-screenshots ## Capture and normalize documentation screenshots.
	@$(SCREENSHOT_NORMALIZE) "$(IMAGE_CONVERT)" \
		"$(SCREENSHOT_MANIFEST)" "$(SCREENSHOT_RAW_DIR)" "$(SCREENSHOT_OUTPUT_DIR)"

.PHONY: check-screenshots
check-screenshots: ## Verify normalized screenshots match the committed raw captures.
	@$(SCREENSHOT_NORMALIZE) "$(IMAGE_CONVERT)" \
		"$(SCREENSHOT_MANIFEST)" "$(SCREENSHOT_RAW_DIR)" "$(SCREENSHOT_OUTPUT_DIR)" --check

##@ Dependencies

.PHONY: node-dependencies
node-dependencies: $(NODE_DEPENDENCIES_STAMP) ## Install locked Node.js development dependencies.

$(NODE_DEPENDENCIES_STAMP): $(DOCS_DIR)/package.json $(DOCS_DIR)/package-lock.json
	PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1 $(NPM) --prefix $(DOCS_DIR) ci
	@touch $@

.PHONY: playwright-browser
playwright-browser: $(PLAYWRIGHT_BROWSER_STAMP) ## Install Chromium used for documentation screenshots.

$(PLAYWRIGHT_BROWSER_STAMP): $(DOCS_DIR)/package-lock.json $(NODE_DEPENDENCIES_STAMP)
	@mkdir -p "$(PLAYWRIGHT_BROWSERS_PATH)"
	PLAYWRIGHT_BROWSERS_PATH="$(abspath $(PLAYWRIGHT_BROWSERS_PATH))" \
		"$(abspath $(PLAYWRIGHT))" install chromium
	@touch "$@"

.PHONY: playwright-browser-ci
playwright-browser-ci: node-dependencies ## Install Chromium and system dependencies for browser tests in CI.
	@mkdir -p "$(PLAYWRIGHT_BROWSERS_PATH)"
	PLAYWRIGHT_BROWSERS_PATH="$(abspath $(PLAYWRIGHT_BROWSERS_PATH))" \
		"$(abspath $(PLAYWRIGHT))" install --with-deps chromium
	@touch "$(PLAYWRIGHT_BROWSER_STAMP)"

.PHONY: dev-tools
dev-tools: $(DEV_PORT) $(OPEN_BROWSER) $(DEV_TAG) $(MAKE_HELP) $(GO_INSTALL_TOOL) $(GITHUB_RELEASE_INSTALL) ## Download the pinned development tools.

.PHONY: golangci-lint
golangci-lint: $(GO_INSTALL_TOOL) ## Download golangci-lint locally if necessary.
	@$(GO_INSTALL_TOOL) \
		--target "$(GOLANGCI_LINT)" \
		--package github.com/golangci/golangci-lint/v2/cmd/golangci-lint \
		--tool-version "$(GOLANGCI_LINT_VERSION)"

.PHONY: open
open: ports $(OPEN_BROWSER) ## Open the browser once the application responds.
	$(call run-tool,$(OPEN_BROWSER),"$(SCREENDECK_URL)")
