# Configuration

ScreenDeck accepts command-line flags and environment variables. Environment variables use the `SCREENDECK__` prefix followed by the upper-case flag name with hyphens replaced by underscores.

| Flag                      | Environment variable                | Default                 | Description                                                    |
| ------------------------- | ----------------------------------- | ----------------------- | -------------------------------------------------------------- |
| `--listen-address` / `-a` | `SCREENDECK__LISTEN_ADDRESS`        | `:8080`                 | TCP address used by the HTTP server.                           |
| `--database-path`         | `SCREENDECK__DATABASE_PATH`         | `./data/screendeck.db`  | SQLite database path.                                          |
| `--auth-key-path`         | `SCREENDECK__AUTH_KEY_PATH`         | `./data/auth.key`       | Local encryption-key path.                                     |
| `--base-url`              | `SCREENDECK__BASE_URL`              | `http://localhost:8080` | Public URL used for room invitation links.                     |
| `--plex-url-override`     | `SCREENDECK__PLEX_URL_OVERRIDE`     | empty                   | Runtime override for the discovered Plex server URL.           |
| `--room-ttl`              | `SCREENDECK__ROOM_TTL`              | `24h`                   | How long rooms remain available.                               |
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

## Plex URL override

`plex-url-override` is mainly useful for development, tunnels, or port-forwards. It changes the URL used at runtime without replacing the Plex server identity stored during authorization.
