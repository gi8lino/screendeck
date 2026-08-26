# Architecture

ScreenDeck is a modular monolith. It runs as one Go process and uses one SQLite database, while package boundaries keep HTTP transport, application behavior, persistence, and external media servers separate.

This document describes the intended dependency direction. New code should preserve these boundaries unless an explicit architectural decision replaces them.

## Package relationships

```mermaid
flowchart TD
    cmd[cmd] --> app[app<br/>lifecycle and top-level wiring]

    app --> config[config]
    app --> maintenance[maintenance]
    app --> providers[providers<br/>integration assembly]
    app --> room[room<br/>domain and use cases]
    app --> routes[routes<br/>HTTP composition]
    app --> store[store<br/>SQLite adapter]

    routes --> middleware[middleware]
    routes --> handler[handler<br/>HTTP, JSON, and SSE]

    handler -. narrow interfaces .-> room
    handler -. narrow interfaces .-> media[media<br/>neutral contracts and manager]
    handler -. setup interfaces .-> plex[plex adapter]
    handler -. setup interfaces .-> jellyfin[jellyfin adapter]

    room --> media
    store -. implements room.Store .-> room
    store -. implements auth and media stores .-> media
    store -. persists provider state .-> plex
    store -. persists provider state .-> jellyfin

    providers --> media
    providers --> plex
    providers --> jellyfin
    plex --> media
    jellyfin --> media
```

The arrows show compile-time dependencies or interface implementations. Runtime calls can travel in the opposite direction through interfaces—for example, `room.Service` calls its `room.Store`, whose concrete implementation is `store.Store`.

## Dependency rules

1. `internal/app` owns process lifecycle and top-level dependency wiring. It does not contain HTTP handlers, domain decisions, or persistence queries.
2. `internal/routes` declares paths and composes middleware. It does not implement endpoint behavior.
3. `internal/handler` owns HTTP concerns: authentication input, decoding, validation feedback, status mapping, JSON encoding, and SSE connections. A handler accepts the smallest interface required by that endpoint.
4. `internal/room` owns room models, room errors, use cases, and the persistence contract required by those use cases. It may depend on provider-neutral `media` types, but never on `store`, Plex, Jellyfin, or HTTP packages.
5. `internal/store` implements persistence contracts with SQLite. It may import domain packages to use their models, but domain packages must not import `store`.
6. `internal/media` owns provider-neutral catalog types, the provider interface, and active-provider selection. It must not import a concrete provider.
7. `internal/plex` and `internal/jellyfin` adapt external APIs to `media.Provider` and expose their provider-specific setup operations.
8. `internal/providers` is the composition boundary for media integrations. It constructs concrete providers and registers them with `media.Manager`.
9. Cross-package interfaces belong to the consumer whenever practical. This keeps dependencies narrow and prevents large service containers from spreading through the application.

## Request flows

### Room operation

```text
HTTP request
  -> middleware
  -> handler
  -> room.Service use case
  -> room.Store interface
  -> SQLite store implementation
  -> HTTP response or SSE notification
```

The handler validates transport input. The room service enforces application rules. The store persists and retrieves data without deciding room behavior.

### Catalog operation

```text
room.Service
  -> media.Catalog
  -> media.Manager
  -> active media.Provider
  -> Plex or Jellyfin
```

Room code sees only provider-neutral libraries and items. Provider identifiers, authentication protocols, and upstream response formats stay inside their adapters.

### Provider setup

```text
HTTP handler
  -> Plex or Jellyfin setup interface
  -> provider-specific authentication manager
  -> provider persistence contract
  -> media.Manager activation
```

Provider setup remains explicit because Plex and Jellyfin have different authentication workflows. Their catalog behavior converges behind `media.Provider` only after configuration.

## Adding functionality

### Add an endpoint

1. Add the path in `internal/routes`.
2. Define a narrow consumer-owned interface in `internal/handler` if the endpoint needs a service operation.
3. Implement decoding, field-level validation, authentication, and response handling in the owning handler file.
4. Add handler tests for transport behavior and service tests for application behavior.

Do not pass a general API or service-container struct to the handler.

### Add a room use case

1. Add the operation to the appropriate focused file in `internal/room`.
2. Extend `room.Store` only when the use case requires new persistence behavior.
3. Implement that operation in `internal/store` using room-owned models and errors.
4. Test domain behavior independently from HTTP status mapping.

### Add a media provider

1. Create a provider-specific adapter package.
2. Implement `media.Provider` and keep authentication details inside the adapter.
3. Add only the persistence contracts required by that adapter and implement them in `internal/store`.
4. Register the provider in `internal/providers`.
5. Expose provider-specific setup endpoints without leaking its types into `internal/room`.

### Change the database schema

Add the next contiguous, forward-only migration under `internal/store/migrations`. Never modify a migration that has shipped. Keep SQL and row conversion in `internal/store`; expose domain models through consumer-owned persistence contracts.

## Architectural guardrails

- Prefer direct dependencies over global state or service locators.
- Introduce an interface at a real package boundary or test seam, not for every concrete type.
- Keep errors meaningful in their owning package; translate them to HTTP responses in `internal/handler`.
- Split files by behavior and ownership, while keeping cohesive files in one package.
- Keep generated frontend output out of Go architecture decisions; `web/src` is canonical and `web/dist` is a build artifact.
- Record a short architecture decision record when a change reverses one of these rules, introduces another process or datastore, or changes the provider model.

The goal is not to maximize the number of layers. It is to keep each decision in one obvious place and make dependency direction visible from imports.
