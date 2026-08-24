---
hide:
  - navigation
---

# Configuration

ScreenDeck accepts command-line flags and environment variables. Environment variables use the `SCREENDECK__` prefix followed by the upper-case flag name with hyphens replaced by underscores.

| Flag                      | Environment variable                | Default                 | Description                                                    |
| ------------------------- | ----------------------------------- | ----------------------- | -------------------------------------------------------------- |
| `--listen-address` / `-a` | `SCREENDECK__LISTEN_ADDRESS`        | `:8080`                 | TCP address used by the HTTP server.                           |
| `--database-path`         | `SCREENDECK__DATABASE_PATH`         | `./data/screendeck.db`  | SQLite database path. Use `:memory:` for ephemeral storage.    |
| `--auth-key-path`         | `SCREENDECK__AUTH_KEY_PATH`         | `./data/auth.key`       | Local encryption-key path. Ignored for an in-memory database.  |
| `--base-url`              | `SCREENDECK__BASE_URL`              | `http://localhost:8080` | Public URL used for room invitation links.                     |
| `--exclude-libraries`     | `SCREENDECK__EXCLUDE_LIBRARIES`     | empty                   | Media library titles or keys excluded from room creation.      |
| `--plex-url-override`     | `SCREENDECK__PLEX_URL_OVERRIDE`     | empty                   | Runtime override for the discovered Plex server URL.           |
| `--room-ttl`              | `SCREENDECK__ROOM_TTL`              | `24h`                   | How long rooms and their saved memberships remain available.   |
| `--room-cleanup-interval` | `SCREENDECK__ROOM_CLEANUP_INTERVAL` | `1h`                    | How often expired rooms are deleted.                           |
| `--log-format` / `-l`     | `SCREENDECK__LOG_FORMAT`            | `json`                  | Log format: `json` or `text`.                                  |
| `--debug` / `-d`          | `SCREENDECK__DEBUG`                 | `false`                 | Enables verbose request and diagnostic logging.                |
| `--experimental`          | `SCREENDECK__EXPERIMENTAL`          | `false`                 | Enables experimental features such as Plex JWT authentication. |

Use the built-in help for the exact configuration supported by the running version:

```sh
go run ./cmd --help
```

## Base URL

`base-url` should be the URL that room participants can open from their devices. For a home server this is usually a LAN hostname or IP address rather than `localhost`.

Examples:

```text
http://192.168.1.10:8080
https://screendeck.home.example
```

## Database storage

By default, ScreenDeck stores its application state in a SQLite database on disk.

For temporary or development deployments, set `database-path` to `:memory:`:

```sh
go run ./cmd --database-path :memory:
```

The equivalent environment variable is:

```sh
SCREENDECK__DATABASE_PATH=:memory:
```

In this mode, both the SQLite database and its encryption key exist only in memory. `auth-key-path` is ignored and no database or authentication-key file is created.

All application state is lost when ScreenDeck stops or restarts, including media-server authorization, rooms, participants, votes, and saved room memberships.

## Saved rooms and room lifetime

ScreenDeck remembers the rooms created or joined by each browser profile and displays active memberships under **Your rooms** on the startup page.

Opening a saved room restores the same participant session. This preserves participant identity, votes, host status, and the room's current round instead of creating a duplicate participant.

`room-ttl` controls the lifetime of the room itself. Saved membership is useful only while that room remains active. Once a room expires and cleanup removes it, it can no longer be resumed and disappears from **Your rooms**.

Saved room discovery is browser-profile-specific. ScreenDeck does not provide user accounts or automatically synchronize memberships between browsers or devices.

## Media provider

ScreenDeck supports **Plex** and **Jellyfin**. Choose the provider from the startup screen after a fresh installation. One provider is active per ScreenDeck installation so rooms, cached catalog metadata, item identifiers, and poster references always belong to one media-server namespace. Switching providers on an existing application database is intentionally not supported.

Plex authorization uses Plex's browser flow. Jellyfin setup asks for the server URL, username, and password. The Jellyfin password is used only for the login request; ScreenDeck persists the returned access token instead. Provider credentials are encrypted with the local authentication key.

## Excluded media libraries

`exclude-libraries` hides selected media libraries from ScreenDeck room creation. Each configured value matches either a library title or its provider-specific key. Matching is case-insensitive and surrounding whitespace is ignored.

Excluded libraries are omitted from the library list shown when creating a room. The backend also rejects an excluded library key if a client submits it directly, so the setting is enforced server-side rather than only hidden in the browser.

Changing the exclusion list does not rewrite existing rooms. Active rooms continue using the deck that was persisted when they were created.

Values can be supplied as a comma-separated list or by repeating the flag. For example:

```sh
go run ./cmd --exclude-libraries "Kids,Archive"
```

The equivalent environment variable is:

```sh
SCREENDECK__EXCLUDE_LIBRARIES=Kids,Archive
```

## Plex URL override

`plex-url-override` is mainly useful for development, tunnels, or port-forwards. It changes the URL used at runtime without replacing the Plex server identity stored during authorization.
