# Deployment

ScreenDeck is designed to run as a single application instance with persistent storage for its SQLite database and Plex authentication key.

## Docker Compose

The repository includes a [`deploy/compose.yaml`](https://github.com/gi8lino/screendeck/blob/main/deploy/compose.yaml) example.

From the repository root:

```sh
cp .env.example .env
```

Set `SCREENDECK_BASE_URL` in `.env` to the URL devices on your network will use, then start ScreenDeck:

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

The database contains encrypted Plex credentials and the key file is required to decrypt them.

## Remote access

ScreenDeck is primarily intended for a trusted home/private network. If it must be reachable remotely, put it behind HTTPS and appropriate access controls or use a private mesh VPN. Avoid exposing the plain HTTP listener directly to the public internet.
