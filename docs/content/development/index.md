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
web/src              canonical frontend source: HTML, templates, CSS, JavaScript, and favicon
web/dist             generated, ignored browser assets embedded by Go
deploy               deployment examples
docs                 documentation project, sources, assets, and tooling dependencies
scripts/web           frontend distribution build
scripts/screenshots   documentation screenshot capture and normalization helpers
```

## Frontend rendering

The browser frontend stays dependency-free. Everything developers edit lives under `web/src/`: the page shell, native HTML `<template>` partials, CSS, JavaScript modules, and favicon. `make web` assembles the templates into `web/dist/index.html` and copies the remaining assets into `web/dist/`.

Treat `web/dist/` as generated output and do not edit or commit it. The directory is ignored by Git and recreated whenever a supported build, test, lint, or run target needs the files consumed by `go:embed`. CI builds the frontend before Go analysis, the Dockerfile generates it inside the builder image, and GoReleaser runs `make web` before compiling release binaries.

JavaScript clones native templates through `instantiateTemplate` and `templateElement` in `web/src/js/ui.js`, then binds API data and event handlers to elements marked with `data-ref`. Static and repeated UI structure belongs in HTML templates; JavaScript updates values with `textContent`, attributes, classes, and event handlers. The old generic `el()` DOM-construction helper is no longer used.

Use the Makefile targets rather than invoking Go commands directly from a fresh checkout, because the generated `web/dist/` tree must exist before packages containing the embedded frontend are loaded:

```sh
make web
make check-web
make test
make build
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

## Database migrations

ScreenDeck uses ordered, forward-only SQLite migrations stored in `internal/store/migrations`. The migration runner embeds these files into the binary and tracks the applied schema with SQLite's `PRAGMA user_version`.

A fresh database starts at version `0` and runs every migration in order. An existing versioned database runs only migrations newer than its current version. Databases created by a newer ScreenDeck build are rejected rather than downgraded.

The first migration contains the complete initial schema:

```text
internal/store/
├── store.go
├── schema.go
├── migrations.go
└── migrations/
    └── 001_initial.sql
```

When the schema changes, add the next numbered migration such as `002_add_room_settings.sql`. Migration numbers must remain contiguous, and migrations that have shipped must not be changed or removed.

Pre-release databases from before the migration system are intentionally not migrated. A non-empty database with `user_version = 0` is rejected and should be recreated.

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
