---
hide:
  - navigation
---

# Development

ScreenDeck is a Go application with an embedded, dependency-free browser frontend. The module currently targets Go 1.25.

Python 3 with `venv` support is only needed for the MkDocs documentation site. Node.js is used for Prettier and Playwright-based screenshot capture. ImageMagick normalizes the committed documentation screenshots.

## Get started

Install the locked Go and Node.js dependencies:

```sh
make download
```

Run the tests and start the application:

```sh
make test
make run
```

Useful development targets:

```sh
make build
make test-race
make cover
make lint
make lint-fix
```

## Project layout

```text
cmd                  application entry point
internal/app         process lifecycle and dependency wiring
internal/config      CLI and environment configuration
internal/handler     HTTP, JSON, and SSE handlers
internal/logging     structured logging and request context
internal/maintenance background housekeeping jobs
internal/middleware  HTTP middleware
internal/plex        Plex authorization and catalog client
internal/room        room orchestration and live notifications
internal/routes      route table and middleware wiring
internal/store       SQLite schema and persistence
web                  embedded browser application
deploy               deployment examples
docs                 documentation project, sources, assets, and tooling dependencies
scripts/screenshots  documentation screenshot capture and normalization helpers
```

## Formatting and checks

Go code is formatted with `gofmt`. Markdown, YAML, and JSON are formatted with the repository-pinned Prettier version.

```sh
make fmt
make lint
make vet
make test
```

The CI workflow also runs `staticcheck` and `git diff --check`.

## Database schema during development

The ScreenDeck database schema is currently pre-release and is not backward compatible. Fresh databases are created at the schema version expected by the running build.

If an existing database is unversioned or uses a different schema version, ScreenDeck refuses to start instead of modifying it automatically. During active development, recreate the database or deployment data volume after a schema-breaking change.

Once stable releases require database upgrades, ScreenDeck can introduce ordered, forward-only migrations starting from the current versioned schema.

## Documentation site

All MkDocs-specific files live under `docs/`:

```text
docs/
├── mkdocs.yml
├── requirements.txt
├── package.json
├── package-lock.json
├── content/
└── screenshots/
```

The Makefile creates the documentation virtual environment at `docs/.venv/` and the generated site at `docs/.site/`.

Node.js development dependencies are installed into `docs/node_modules/`.

Start the local documentation server:

```sh
make docs
```

Build the same strict static site used by GitHub Pages:

```sh
make docs-build
```

GitHub Actions validates documentation changes on pull requests and publishes changes from `main` to:

```text
https://gi8lino.github.io/screendeck/
```

The repository must have **Settings → Pages → Build and deployment → Source** set to **GitHub Actions**.

## Documentation navigation

The main ScreenDeck documentation sections are already available as tabs in the MkDocs Material header. The top-level pages therefore hide Material's left navigation sidebar with page metadata so the current section title is not repeated in a one-item sidebar.

Nested documentation sections can still enable normal navigation when they eventually contain multiple pages.

## Documentation screenshots

The screenshot workflow separates browser capture from committed documentation assets:

```text
Playwright demo capture
        ↓
docs/screenshots/raw/*.png
        ↓
docs/screenshots/screenshots.manifest
        ↓
ImageMagick normalization
        ↓
docs/content/assets/screenshots/*.png
```

Regenerate the screenshots with:

```sh
make screenshots
```

The first invocation installs the Playwright Chromium build into:

```text
docs/.playwright/
```

This browser cache belongs only to ScreenDeck and is ignored by Git. It does not use Playwright's operating-system-wide browser cache, so other projects using different Playwright versions cannot replace ScreenDeck's Chromium installation.

The Chromium installation is tied to `docs/package-lock.json`. Updating the locked Playwright version causes the browser target to run again. Normal `make screenshots` invocations reuse the already installed browser without running `playwright install chromium` again.

The target starts a local demo server that uses the real ScreenDeck frontend with deterministic fixture data. Room `DECK42` contains **Host**, **Alice**, and **Bob**. The demo server never contacts Plex and does not touch the normal ScreenDeck database.

Raw captures are kept in `docs/screenshots/raw/`. Crop geometry and optional padding are declared in `docs/screenshots/screenshots.manifest`. ImageMagick strips non-deterministic PNG metadata before writing the published assets.

To capture only the browser images:

```sh
make capture-screenshots
```

To explicitly install or repair the local Chromium installation:

```sh
rm -rf docs/.playwright
make playwright-browser
```

To verify that the normalized screenshots still match their raw captures:

```sh
make check-screenshots
```

`make printscreens` remains an alias for `make screenshots`.

By default the normalization target expects the ImageMagick 7 `magick` command. On systems that expose ImageMagick as `convert`, override it:

```sh
make screenshots IMAGE_CONVERT=convert
```

## Developing against a forwarded Plex server

If Plex is reachable through a local port-forward, keep normal Plex authorization and override only the runtime server URL:

```sh
kubectl -n media port-forward service/plex 32400:32400
go run ./cmd --plex-url-override http://127.0.0.1:32400 --debug --log-format text
```

The override is not persisted. Supply it again whenever ScreenDeck starts with the port-forward.

## Release builds

GoReleaser builds Linux `amd64` and `arm64` binaries and publishes the container image. The Makefile also provides `patch`, `minor`, `major`, and `push` targets for repository tag workflows.
