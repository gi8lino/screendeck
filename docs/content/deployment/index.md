---
hide:
  - navigation
---

# Deployment

ScreenDeck is designed to run as a single application instance. By default, its SQLite database and authentication encryption key are stored persistently, while an in-memory database can be used for temporary deployments.

## Docker Compose

The repository includes a [`deploy/compose.yaml`](https://github.com/gi8lino/screendeck/blob/main/deploy/compose.yaml) example.

Start ScreenDeck from the repository root:

```sh
docker compose -f deploy/compose.yaml up -d
```

By default, ScreenDeck is available at:

```text
http://localhost:8080
```

If other devices should access ScreenDeck, set `SCREENDECK__BASE_URL` to the URL they will use. This URL is used for room invitation links and does not change the address ScreenDeck listens on:

```sh
SCREENDECK__BASE_URL=http://192.168.1.10:8080 \
  docker compose -f deploy/compose.yaml up -d
```

The Compose deployment stores the database and authentication key in the `screendeck-data` volume.

ScreenDeck does not require Plex or Jellyfin credentials through environment variables. Plex credentials are obtained through the Plex authorization flow. Jellyfin setup exchanges the supplied username and password for an access token and does not persist the password. Persisted provider tokens are encrypted in the database.

See [Configuration](../configuration/index.md) for the available runtime settings.

ScreenDeck activates one media provider per installation. Choose Plex or Jellyfin during initial setup; the selected provider is stored with the rest of the application state.

## Kubernetes

Example manifests live in [`deploy/kubernetes`](https://github.com/gi8lino/screendeck/tree/main/deploy/kubernetes). They include:

- a namespace,
- a persistent volume claim,
- a single-replica deployment,
- a service,
- and a Kustomize file.

Before applying them, edit `SCREENDECK__BASE_URL` in `deployment.yaml` to an address your users can reach. Then run:

```sh
kubectl apply -k deploy/kubernetes
```

The example service is `ClusterIP`. Expose it using an Ingress, Gateway, private load balancer, port-forward, or another method appropriate for your cluster.

ScreenDeck currently uses SQLite, so the example intentionally runs one replica. Do not scale the deployment horizontally while all replicas share the same SQLite database.

## Persistent data

The default deployments store ScreenDeck state persistently.

Keep these two files together when backing up or restoring ScreenDeck:

- `/data/screendeck.db`
- `/data/auth.key`

The SQLite database contains application state including the active media provider, Plex or Jellyfin authorization, active rooms, participants, votes, browser identity hashes, and saved room memberships.

The authentication key encrypts sensitive values stored in the database, including media-provider credentials and the participant session tokens used to resume saved rooms. Restoring `screendeck.db` without the matching `auth.key` prevents those encrypted values from being recovered.

Browser identity cookies remain on users' browsers and are not part of the server backup. After a server restore, a browser can rediscover its saved rooms when its existing identity cookie still matches the restored membership data.

## In-memory database

For temporary or development deployments, ScreenDeck can use an in-memory SQLite database:

```sh
SCREENDECK__DATABASE_PATH=:memory: \
  docker compose -f deploy/compose.yaml up -d
```

In this mode, the SQLite database and encryption key exist only in memory. No `screendeck.db` or `auth.key` file is created.

All ScreenDeck state is lost when the application stops or restarts, including media-server authorization, rooms, participants, votes, and saved room memberships.

Persistent storage is therefore recommended for normal deployments.

## Remote access

ScreenDeck is primarily intended for a trusted home or private network.

If ScreenDeck is reachable remotely, put it behind HTTPS and appropriate access controls or use a private mesh VPN. Avoid exposing the plain HTTP listener directly to the public internet. ScreenDeck has no built-in user authentication for setup and room APIs, so it emits a startup/browser warning whenever its listener is bound beyond loopback.

If ScreenDeck is exposed through a reverse proxy, set `SCREENDECK__BASE_URL` to the external HTTPS URL, for example `https://screendeck.example.com`.
