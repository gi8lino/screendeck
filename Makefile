# Makefile

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

NODE_BIN ?= ./node_modules/.bin
NODE_DEPENDENCIES_STAMP := node_modules/.screendeck-dependencies-installed
PRETTIER ?= $(NODE_BIN)/prettier
PLAYWRIGHT ?= $(NODE_BIN)/playwright
IMAGE_CONVERT ?= magick

PYTHON ?= python3
DOCS_DIR := docs
DOCS_CONFIG := $(DOCS_DIR)/mkdocs.yml
DOCS_REQUIREMENTS := $(DOCS_DIR)/requirements.txt
DOCS_VENV := $(DOCS_DIR)/.venv
DOCS_PYTHON := $(DOCS_VENV)/bin/python
DOCS_DEPENDENCIES_STAMP := $(DOCS_VENV)/.requirements-installed
DOCS_SITE := $(DOCS_DIR)/.site

SCREENSHOT_MANIFEST := $(DOCS_DIR)/screenshots/screenshots.manifest
SCREENSHOT_RAW_DIR := $(DOCS_DIR)/screenshots/raw
SCREENSHOT_OUTPUT_DIR := $(DOCS_DIR)/content/assets/screenshots
SCREENSHOT_CAPTURE := scripts/screenshots/capture.sh
SCREENSHOT_NORMALIZE := scripts/screenshots/normalize.sh

PRETTIER_MD_SOURCES := README.md "docs/content/**/*.md"
PRETTIER_YAML_SOURCES := \
	".github/**/*.{yml,yaml}" \
	"deploy/**/*.{yml,yaml}" \
	"docs/mkdocs.yml" \
	"docs/content/.nav.yml" \
	"docs/content/**/.nav.yml"
PRETTIER_JSON_SOURCES := ".github/**/*.json"

## Tool Binaries
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint

## Tool Versions
# renovate: datasource=github-releases depName=golangci/golangci-lint
GOLANGCI_LINT_VERSION ?= v2.13.0

## Build Configuration
BINARY ?= screendeck
COMMAND ?= ./cmd
BUILD_VERSION ?= dev
BUILD_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS ?= -s -w -X main.Version=$(BUILD_VERSION) -X main.Commit=$(BUILD_COMMIT)

# Default tag prefix. Override with an empty value for unprefixed tags.
VERSION_PREFIX ?= v

##@ Tagging

# Find the latest tag with the configured prefix, or use 0.0.0 when none exists.
LATEST_TAG = $(shell git tag --list "$(VERSION_PREFIX)*" --sort=-v:refname | head -n 1)
VERSION = $(shell [ -n "$(LATEST_TAG)" ] && echo $(LATEST_TAG) | sed "s/^$(VERSION_PREFIX)//" || echo "0.0.0")

.PHONY: patch
patch: ## Create a new patch release (x.y.Z+1)
	@NEW_VERSION=$$(echo "$(VERSION)" | awk -F. '{printf "%d.%d.%d", $$1, $$2, $$3+1}') && \
	git tag "$(VERSION_PREFIX)$${NEW_VERSION}" && \
	echo "Tagged $(VERSION_PREFIX)$${NEW_VERSION}"

.PHONY: minor
minor: ## Create a new minor release (x.Y+1.0)
	@NEW_VERSION=$$(echo "$(VERSION)" | awk -F. '{printf "%d.%d.0", $$1, $$2+1}') && \
	git tag "$(VERSION_PREFIX)$${NEW_VERSION}" && \
	echo "Tagged $(VERSION_PREFIX)$${NEW_VERSION}"

.PHONY: major
major: ## Create a new major release (X+1.0.0)
	@NEW_VERSION=$$(echo "$(VERSION)" | awk -F. '{printf "%d.0.0", $$1+1}') && \
	git tag "$(VERSION_PREFIX)$${NEW_VERSION}" && \
	echo "Tagged $(VERSION_PREFIX)$${NEW_VERSION}"

.PHONY: tag
tag: ## Show the latest tag.
	@echo "Latest version: $(LATEST_TAG)"

.PHONY: push
push: ## Push tags to the configured remote.
	git push --tags

##@ Development

.PHONY: download
download: node-dependencies ## Download Go and Node.js dependencies.
	go mod download

.PHONY: run
run: ## Run ScreenDeck locally.
	go run $(COMMAND)

.PHONY: build
build: ## Build the ScreenDeck binary.
	go build -ldflags="$(LDFLAGS)" -o $(BINARY) $(COMMAND)

.PHONY: vet
vet: ## Run Go static analysis.
	go vet ./...

.PHONY: test
test: vet ## Run backend unit tests.
	go test -covermode=atomic -count=1 -parallel=4 -timeout=5m ./...

.PHONY: test-race
test-race: vet ## Run backend unit tests with the race detector.
	go test -race -count=1 -parallel=4 -timeout=5m ./...

.PHONY: cover
cover: ## Display test coverage.
	go test -coverprofile=coverage.out -covermode=atomic -count=1 -parallel=4 -timeout=5m ./...
	go tool cover -html=coverage.out

.PHONY: clean
clean: ## Clean up generated application and documentation files.
	rm -f $(BINARY) coverage.out coverage.html
	rm -rf $(DOCS_SITE)

##@ Formatting

.PHONY: fmt
fmt: fmt-go fmt-md fmt-yaml fmt-json ## Format all supported files.

.PHONY: fmt-go
fmt-go: ## Format Go code.
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
lint: lint-go lint-md lint-yaml lint-json ## Run all linters and formatting checks.

.PHONY: lint-go
lint-go: golangci-lint ## Run golangci-lint.
	$(GOLANGCI_LINT) run

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint and apply fixes.
	$(GOLANGCI_LINT) run --fix

.PHONY: lint-md
lint-md: node-dependencies ## Check Markdown formatting.
	@$(PRETTIER) --check $(PRETTIER_MD_SOURCES)

.PHONY: lint-yaml
lint-yaml: node-dependencies ## Check YAML formatting.
	@$(PRETTIER) --check $(PRETTIER_YAML_SOURCES)

.PHONY: lint-json
lint-json: node-dependencies ## Check JSON formatting.
	@$(PRETTIER) --check $(PRETTIER_JSON_SOURCES)

##@ Documentation

.PHONY: docs
docs: $(DOCS_DEPENDENCIES_STAMP) ## Serve the MkDocs site locally.
	$(DOCS_PYTHON) -m mkdocs serve -f $(DOCS_CONFIG)

PHONY: docs-build
docs-build: $(DOCS_DEPENDENCIES_STAMP) ## Build the MkDocs site with strict validation.
	$(DOCS_PYTHON) -m mkdocs build --strict -f $(DOCS_CONFIG)

.PHONY: capture-screenshots
capture-screenshots: playwright-browser ## Capture raw demo screenshots with Playwright.
	@PLAYWRIGHT=$(PLAYWRIGHT) $(SCREENSHOT_CAPTURE)

.PHONY: screenshots
screenshots: capture-screenshots ## Capture and normalize documentation screenshots.
	@$(SCREENSHOT_NORMALIZE) "$(IMAGE_CONVERT)" \
		"$(SCREENSHOT_MANIFEST)" "$(SCREENSHOT_RAW_DIR)" "$(SCREENSHOT_OUTPUT_DIR)"

.PHONY: printscreens
printscreens: screenshots ## Alias for screenshots.

.PHONY: check-screenshots
check-screenshots: ## Verify normalized screenshots match the committed raw captures.
	@$(SCREENSHOT_NORMALIZE) "$(IMAGE_CONVERT)" \
		"$(SCREENSHOT_MANIFEST)" "$(SCREENSHOT_RAW_DIR)" "$(SCREENSHOT_OUTPUT_DIR)" --check

##@ Dependencies

.PHONY: node-dependencies
node-dependencies: $(NODE_DEPENDENCIES_STAMP) ## Install locked Node.js development dependencies.

$(NODE_DEPENDENCIES_STAMP): docs/package.json docs/package-lock.json
	npm --prefix docs ci
	@touch $(NODE_DEPENDENCIES_STAMP)

.PHONY: playwright-browser
playwright-browser: node-dependencies ## Install Chromium used for documentation screenshots.
	$(PLAYWRIGHT) install chromium

$(DOCS_PYTHON):
	$(PYTHON) -m venv $(DOCS_VENV)

$(DOCS_DEPENDENCIES_STAMP): $(DOCS_REQUIREMENTS) | $(DOCS_PYTHON)
	$(DOCS_PYTHON) -m pip install --upgrade pip
	$(DOCS_PYTHON) -m pip install -r $(DOCS_REQUIREMENTS)
	@touch $(DOCS_DEPENDENCIES_STAMP)

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.

$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))

# go-install-tool installs a versioned tool and links its stable binary name.
# $1 - target path with name of binary
# $2 - package URL
# $3 - version
define go-install-tool
@[ -f "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f $(1) || true ;\
GOBIN=$(LOCALBIN) go install $${package} ;\
mv $(1) $(1)-$(3) ;\
} ;\
ln -sf $(1)-$(3) $(1)
endef

##@ General

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)
