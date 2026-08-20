# Development

ScreenDeck is a Go application with an embedded, dependency-free browser frontend. The module currently targets Go 1.25.

## Get started

Download dependencies, run the tests, and start the application:

```sh
make download
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
docs                 project documentation and screenshots
scripts              development/documentation helpers
```

## Formatting and checks

Go code is formatted with `gofmt`. Markdown, YAML, and JSON are formatted with Prettier through the Makefile.

```sh
make fmt
make lint
make vet
make test
```

The CI workflow also runs `staticcheck` and `git diff --check`.

## Create documentation screenshots

Run:

```sh
make screenshots
```

The target starts a local demo server that uses the real ScreenDeck frontend with deterministic fixture data. It creates room `DECK42` with a host plus **Alice** and **Bob**, then captures:

- `docs/screenshots/home.png`
- `docs/screenshots/room.png`
- `docs/screenshots/winner.png`

The target uses Playwright through `npx`; the Playwright Chromium build is installed on the first run and cached for later runs. Node.js and `curl` are required for this documentation-only workflow.

The demo server never contacts Plex and does not touch the normal ScreenDeck database.

## Developing against a forwarded Plex server

If Plex is reachable through a local port-forward, keep normal Plex authorization and override only the runtime server URL:

```sh
kubectl -n media port-forward service/plex 32400:32400
go run ./cmd --plex-url-override http://127.0.0.1:32400 --debug --log-format text
```

The override is not persisted. Supply it again whenever ScreenDeck starts with the port-forward.

## Release builds

GoReleaser builds Linux `amd64` and `arm64` binaries and publishes the container image. The Makefile also provides `patch`, `minor`, `major`, and `push` targets for repository tag workflows.
