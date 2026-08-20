---
hide:
  - navigation
---

# Deployment

ScreenDeck is designed to run as a single application instance with persistent storage for its SQLite database and authentication encryption key.

## Docker Compose

The repository includes a [`deploy/compose.yaml`](https://github.com/gi8lino/screendeck/blob/main/deploy/compose.yaml) example.

From the repository root:

```sh
cp .env.example .env
```

Set `SCREENDECK__BASE_URL` in `.env` to the URL devices on your network will use, then start ScreenDeck:

```sh
docker compose -f deploy/compose.yaml up -d
```

The Compose deployment stores the database and authentication key in the `screendeck-data` volume.

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

Keep these two files together when backing up or restoring ScreenDeck:

- `/data/screendeck.db`
- `/data/auth.key`

The SQLite database contains application state including Plex configuration, active rooms, participants, votes, browser identity hashes, and saved room memberships.

The authentication key encrypts sensitive values stored in the database, including Plex credentials and the participant session tokens used to resume saved rooms. Restoring `screendeck.db` without the matching `auth.key` prevents those encrypted values from being recovered.

Browser identity cookies remain on the users' browsers and are not part of the server backup. After a server restore, a browser can rediscover its saved rooms when its existing identity cookie still matches the restored membership data.

## Remote access

ScreenDeck is primarily intended for a trusted home/private network. If it must be reachable remotely, put it behind HTTPS and appropriate access controls or use a private mesh VPN. Avoid exposing the plain HTTP listener directly to the public internet.
