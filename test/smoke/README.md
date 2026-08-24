# Local media-provider smoke environment

This Compose stack runs ScreenDeck, Jellyfin, and Plex together for local end-to-end validation. It is intentionally not part of CI.

## Generate synthetic media

For a repeatable smoke library, generate small valid MP4 fixtures with FFmpeg:

```sh
make smoke-media
```

The command creates deterministic fixtures under `test/smoke/media/generated/`:

```text
test/smoke/media/generated/
├── movies/
│   ├── Smoke Action (2020)/
│   ├── Smoke Comedy (2021)/
│   ├── Smoke Drama (2022)/
│   ├── Smoke Horror (2023)/
│   ├── Smoke Old Movie (1985)/
│   └── Smoke Long Movie (2024)/
└── tv/
    └── Smoke Show/
        └── Season 01/
            ├── Smoke Show - S01E01.mp4
            └── Smoke Show - S01E02.mp4
```

Each movie has a valid synthetic video, a local `poster.jpg`, and a small NFO sidecar. The fixtures intentionally use different years, genres, and durations so provider behavior and ScreenDeck filters are easy to inspect. The TV fixture includes a show poster, show metadata, and two episodes.

The videos contain only generated solid-color frames and no copyrighted media. They use a very low frame rate so even movie-length fixtures stay small and generate quickly. They are intended for metadata scanning and ScreenDeck smoke tests, not playback-quality testing.

`ffmpeg` must be available in `PATH`. Override the binary if needed:

```sh
make smoke-media FFMPEG=/path/to/ffmpeg
```

Override the generated media directory when needed:

```sh
make smoke-media SMOKE_MEDIA_DIR=/path/to/media
```

The generator can also be called directly with the output directory as its
only argument. Without an argument, it defaults to
`test/smoke/media/generated/`.

Generated media is ignored by Git. Remove only these generated fixtures with:

```sh
make smoke-media-clean
```

You can still put your own test media anywhere below `test/smoke/media/`; the cleanup target only removes `test/smoke/media/generated/`.

## Optional environment

The stack works with the default Jellyfin and Plex images. To override image tags, timezone, or provide a Plex claim token, copy the example environment file:

```sh
cp test/smoke/.env.example test/smoke/.env
```

For a fresh Plex configuration, add a short-lived `PLEX_CLAIM` value immediately before the first `make smoke-up`. The claim token is only needed while Plex is initially associated with the test account.

## Start the stack

```sh
make smoke-up
```

The services are available at:

| Service    | URL                        |
| ---------- | -------------------------- |
| ScreenDeck | http://localhost:8080      |
| Jellyfin   | http://localhost:8096      |
| Plex       | http://localhost:32400/web |

### Jellyfin setup

Complete Jellyfin's first-run wizard, create a local test user, and add the generated libraries using these container paths:

```text
/media/generated/movies
/media/generated/tv
```

If you use your own media instead, choose its corresponding path below `/media`.

When connecting Jellyfin from ScreenDeck, use this server URL:

```text
http://jellyfin:8096
```

ScreenDeck runs in the same Compose network, so `localhost:8096` would point back to the ScreenDeck container rather than Jellyfin.

### Plex setup

Complete the Plex server setup and add the generated movie and TV libraries using:

```text
/media/generated/movies
/media/generated/tv
```

The smoke stack sets ScreenDeck's Plex URL override to `http://plex:32400`. Plex account authorization still uses the normal Plex authorization flow, but catalog traffic from ScreenDeck is sent directly to the local Plex container.

Provider metadata agents can interpret local and embedded metadata differently. The generated filenames, durations, and local posters are deterministic; richer metadata such as genre handling should still be checked against what each provider actually exposes to ScreenDeck.

## Smoke checklist

Validate the same core behavior with both providers:

- configure and select the provider;
- discover movie and TV libraries;
- create a room from selected libraries;
- verify fixture titles, years, durations, and posters;
- inspect the genres exposed by the provider;
- exercise room filters and personal genre preferences where metadata is available;
- vote from multiple browser sessions and produce a match;
- start another round and verify the narrowed deck;
- restart the stack and verify ScreenDeck restores its database and provider credentials.

For restart persistence, use:

```sh
make smoke-down
make smoke-up
```

The normal down target preserves all named volumes.

## Stop or reset

Stop the containers while preserving ScreenDeck, Jellyfin, and Plex state:

```sh
make smoke-down
```

Follow logs:

```sh
make smoke-logs
```

Remove the containers and all smoke-state volumes, while keeping the files in `test/smoke/media/`:

```sh
make smoke-reset
```

After a reset, Jellyfin and Plex require their initial setup again.
