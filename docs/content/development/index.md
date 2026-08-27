---
hide:
  - navigation
---

# Development

ScreenDeck is a Go application with an embedded, dependency-free browser frontend. The module currently targets Go 1.25.

See [Architecture](architecture.md) for package boundaries, dependency rules, and extension guidance.

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
internal/media       provider-neutral media types, provider interface, and active-provider manager
internal/providers   construction and assembly of Plex and Jellyfin integrations
internal/plex        Plex authorization and catalog adapter
internal/jellyfin    Jellyfin authorization and catalog adapter
internal/room        room orchestration and live notifications
internal/routes      route table and middleware wiring
internal/store       SQLite schema and persistence
web/src              canonical frontend source: HTML, templates, CSS, JavaScript, and favicon
web/dist             generated, ignored browser assets embedded by Go
deploy               deployment examples
docs                 documentation project, sources, assets, and tooling dependencies
scripts/web          frontend distribution build
scripts/screenshots  documentation screenshot capture and normalization helpers
test/smoke           local ScreenDeck, Jellyfin, and Plex smoke environment
```

## Media-provider architecture

Room orchestration depends only on the provider-neutral catalog contract in `internal/media`. The shared `media.Library` and `media.Item` types contain exactly the metadata ScreenDeck needs for filtering, swiping, persistence, and posters. The room and generic catalog store layers do not import Plex or Jellyfin.

`media.Provider` is the runtime interface implemented by every integration. It combines the common catalog operations with a stable provider ID and configured-state reporting. `media.Manager` accepts provider implementations through that interface, restores the active provider from SQLite, and exposes only the active catalog to `room.Service`.

`internal/plex` and `internal/jellyfin` remain independent adapters. Their authentication and setup methods stay provider-specific, while their authenticated managers also implement `media.Provider`. Compile-time interface assertions in each adapter keep the runtime contract explicit.

`internal/providers` is the composition boundary. It constructs the Plex and Jellyfin services, registers them with `media.Manager`, and returns both the provider-neutral runtime manager and the provider-specific setup services needed by the HTTP handlers. `internal/app` therefore does not construct or register individual providers itself.

ScreenDeck intentionally activates one provider per installation. This keeps persisted library keys, item IDs, cached metadata, and poster references in one provider namespace. A future provider such as Emby can be added by implementing `media.Provider` and registering its constructor in `internal/providers` while keeping its authentication logic separate.

## Local provider smoke tests

The local smoke environment under `test/smoke/` runs ScreenDeck with real Jellyfin and Plex containers. It is intended for manual end-to-end validation and is deliberately not part of the GitHub Actions test workflow. Normal CI continues to use deterministic unit and HTTP test servers instead of external media-server containers.

Generate a deterministic local media library with FFmpeg, then start the stack:

```sh
make smoke-media
make smoke-up
```

The generated fixtures live under `test/smoke/media/generated/` and include six synthetic movies plus a two-episode TV show. They use valid MP4 containers, local posters, and lightweight sidecar metadata without committing binary media to Git. `make smoke-media-clean` removes only the generated fixtures, leaving any manually supplied test media untouched.

ScreenDeck is exposed on port `8080`, Jellyfin on `8096`, and Plex on `32400`. The smoke Compose network lets ScreenDeck contact Jellyfin as `http://jellyfin:8096`; Plex catalog traffic uses the configured `http://plex:32400` runtime override while account authorization still follows the normal Plex flow.

Stop the stack without losing provider configuration or the ScreenDeck database:

```sh
make smoke-down
```

To remove all smoke-state volumes and return Jellyfin, Plex, and ScreenDeck to a fresh state while preserving the media files:

```sh
make smoke-reset
```

Use `make smoke-logs` to follow all three services. See `test/smoke/README.md` for first-run Jellyfin and Plex setup details and the smoke checklist.

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

The first migration contains the original schema and later migrations evolve it:

```text
internal/store/
├── store.go
├── schema.go
├── migrations.go
└── migrations/
    ├── 001_initial.sql
    └── 002_media_providers.sql
```

Migration `002_media_providers.sql` makes the catalog schema provider-neutral, adds the active media-provider marker and Jellyfin authorization storage, and preserves existing Plex installations. When the schema changes again, add the next numbered migration such as `003_add_indexes.sql`. Migration numbers must remain contiguous, and migrations that have shipped must not be changed or removed.

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

The target starts a local demo server that uses the real ScreenDeck frontend with deterministic fixture data. Room `DECK42` contains **Host**, **Alice**, and **Bob**. The demo server never contacts Plex or Jellyfin and does not touch the normal ScreenDeck database.

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

By default the normalization target expects the ImageMagick 7 `magick` command. On systems that expose ImageMagick as `convert`, override it:

```sh
make screenshots IMAGE_CONVERT=convert
```

## Developing against a forwarded Plex server

If Plex is reachable through a local port-forward, keep normal Plex authorization and override only the runtime server URL:

```sh
kubectl -n media port-forward service/plex 32400:32400
go run ./cmd --plex-url-override http://127.0.0.1:32400 --debug --access-log --log-format text
```

The override is not persisted. Supply it again whenever ScreenDeck starts with the port-forward.

## Release builds

GoReleaser builds Linux `amd64` and `arm64` binaries and publishes the container image. The Makefile also provides `patch`, `minor`, `major`, and `push` targets for repository tag workflows.
