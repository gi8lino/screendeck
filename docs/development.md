# Development

ScreenDeck is a Go application with an embedded, dependency-free browser frontend. The module currently targets Go 1.25.

Node.js is used only for development tooling such as Prettier and documentation screenshots. It is not required by the ScreenDeck application at runtime.

## Get started

Install the Go and Node.js dependencies:

```sh
make download
```

Run the tests:

```sh
make test
```

Start the application:

```sh
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
docs                 project documentation and screenshots
scripts              development and documentation helpers
```

## Formatting and checks

Go code is formatted with `gofmt`.

Markdown, YAML, and JSON are formatted with the repository-pinned version of Prettier.

```sh
make fmt
make lint
make vet
make test
```

The CI workflow also runs `staticcheck` and `git diff --check`.

## Node.js dependencies

Development dependencies are declared in the root `package.json` and pinned by `package-lock.json`.

Install exactly the locked versions with:

```sh
npm ci
```

Do not commit `node_modules/`.

When changing a Node.js development dependency, update the lock file with `npm install` and commit both `package.json` and `package-lock.json`.

## Create documentation screenshots

Generate the screenshots with:

```sh
make screenshots
```

The first invocation installs the Chromium build used by Playwright.

The target starts a local demo server that uses the real ScreenDeck frontend with deterministic fixture data. It creates room `DECK42` with a host plus **Alice** and **Bob**, then captures:

- `docs/screenshots/home.png`
- `docs/screenshots/room.png`
- `docs/screenshots/winner.png`

The demo server never contacts Plex and does not touch the normal ScreenDeck database.

`make printscreens` is an alias for `make screenshots`.

If you only want to install the screenshot browser dependency, run:

```sh
make playwright-browser
```

## Developing against a forwarded Plex server

If Plex is reachable through a local port-forward, keep normal Plex authorization and override only the runtime server URL:

```sh
kubectl -n media port-forward service/plex 32400:32400
go run ./cmd --plex-url-override http://127.0.0.1:32400 --debug --log-format text
```

The override is not persisted. Supply it again whenever ScreenDeck starts with the port-forward.

## Release builds

GoReleaser builds Linux `amd64` and `arm64` binaries and publishes the container image.

The Makefile also provides `patch`, `minor`, `major`, and `push` targets for repository tag workflows.
