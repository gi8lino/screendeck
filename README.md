# ScreenDeck

ScreenDeck is a small self-hosted web app for choosing something to watch with friends. A host selects Plex movie and TV libraries, applies optional filters, shares a six-character room code, and everyone swipes independently. A title becomes a match when every participant likes it.

It is designed for a home network: one Go process, one SQLite file, and no accounts for guests. ScreenDeck reads metadata and posters from Plex; it does not stream video or modify the Plex library.

## Features

- Connects through Plex's server-compatible PIN authorization flow.
- Optionally exposes Plex's experimental Ed25519/JWT authorization flow.
- Discovers accessible Plex servers and prefers their local non-relay connection.
- Loads movie and TV-show libraries and metadata from Plex.
- Filters rooms by library, genre, release year, maximum movie runtime, and watched state.
- Lets the host cap the shuffled first round at 50, 100, 250, or 500 titles, or use every matching title; the UI defaults to 250.
- Lets each participant choose optional personal genres that only affect their own swipe deck.
- Touch and mouse swipe interface, with accessible like/pass buttons.
- Temporary rooms with anonymous display names.
- Unanimous matches for two or more participants, with an immediate animated match reveal for the deciding swipe.
- Unanimous next-round requests that can narrow the current matches into a new deck at any time, without waiting for everyone to finish a large round.
- Collapsed match piles that expand on demand instead of filling the room view with every poster.
- Live participant and match updates using Server-Sent Events.
- Plex credentials remain on the server; posters are proxied and cached by the browser.
- Embedded frontend, SQLite persistence, graceful shutdown, and a `/healthz` endpoint.

## Run

Start ScreenDeck and open it in a browser:

```sh
export SCREENDECK__BASE_URL=http://192.168.1.10:8080
go run ./cmd
```

Choose **Connect Plex**, authorize ScreenDeck in Plex's login window, and select a discovered Plex Media Server. ScreenDeck uses the same legacy PIN exchange as Tautulli because current Plex Media Server releases reject tokens produced by Plex's documented JWT flow. No token needs to be copied manually.

Pass `--experimental` to show an additional **Use JWT (experimental)** button. JWT credentials use an Ed25519 device key and are refreshed automatically, but the resulting token may be rejected by Plex Media Server with `401 Unauthorized`.

Open the configured base URL from other devices on the same network. The process creates `./data/screendeck.db` and `./data/auth.key` by default.

Environment variables use the `SCREENDECK__` prefix (two underscores), following `tinyflags` conventions.

All settings can also be passed as flags:

```text
-a, --listen-address ADDR
    --database-path PATH
    --auth-key-path PATH
    --base-url URL
    --plex-url-override URL
    --room-ttl DURATION
    --experimental
-l, --log-format FORMAT
-d, --debug
```

Run `go run ./cmd --help` for current defaults and environment names.

When developing against a Plex server reachable through a Kubernetes port-forward, override only the runtime server URL while keeping normal Plex authorization and discovery:

```sh
kubectl -n media port-forward service/plex 32400:32400
go run ./cmd --plex-url-override http://127.0.0.1:32400 --debug
```

The override is not persisted and must be supplied whenever the application starts with the port-forward.

For detailed Plex discovery and authentication diagnostics, enable text debug logging:

```sh
go run ./cmd --debug --log-format text --plex-url-override http://127.0.0.1:32400
```

The logs include discovered and effective server URLs, connection type, token source labels, HTTP status, and request duration. Plex tokens, PIN codes, JWTs, and private keys are never logged.

## Docker Compose

Copy `.env.example` to `.env`, set the LAN address of the Docker host, and run:

```sh
docker compose up --build -d
```

SQLite data and the authentication encryption key are stored in the `screendeck-data` volume. To access ScreenDeck from other devices, set `SCREENDECK_BASE_URL` to the Docker host's LAN address—not `localhost`.

## Development

The project uses Go 1.25 and a dependency-free browser frontend:

```sh
make test
make run
```

The layout follows the conventions used by [`containeroo/heartbeats`](https://github.com/containeroo/heartbeats): a thin command entry point, application wiring under `internal/app`, feature-oriented internal packages, explicit handlers/routes, embedded web assets, and container-first deployment.

```text
cmd                 application entry point
internal/app        process lifecycle and dependency wiring
internal/config     tinyflags CLI and environment configuration
internal/handler    HTTP and JSON/SSE handlers
internal/logging    structured logger and request context
internal/maintenance background housekeeping jobs
internal/middleware HTTP request middleware
internal/plex       Plex authorization and read-only catalog client
internal/room       room orchestration and live notifications
internal/routes     route table and middleware
internal/store      SQLite schema and queries
web                 embedded browser application
```

## Security notes

- Plex account/server tokens and any experimental Ed25519 private key are encrypted with AES-256-GCM before being written to SQLite.
- The separate `auth.key` file is created with mode `0600`. Back up it and the database together; the encrypted credentials cannot be recovered if the key is lost.
- Plex credentials never reach guest browsers. The temporary setup token only authorizes the server-selection step.
- The default deployment is intended for a trusted LAN.
- For remote friends, put ScreenDeck behind HTTPS and access control, or use a private mesh VPN. Do not expose the plain HTTP port directly to the public internet.
- Room tokens are random, stored as SHA-256 hashes, and kept in each participant's browser storage. Rooms expire after 24 hours by default.

## Current scope

Movies and top-level TV shows are supported. Participants can apply personal genre preferences, hosts can limit the shuffled first-round deck size, and the group can unanimously narrow current matches into repeatable follow-up rounds without finishing the entire deck. Individual seasons/episodes, configurable majority thresholds, permanent profiles, and direct playback are intentionally outside the current version.
