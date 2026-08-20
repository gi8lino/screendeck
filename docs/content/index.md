<p align="center">
  <img src="assets/logo.svg" alt="ScreenDeck" width="480" />
</p>

# ScreenDeck documentation

ScreenDeck is a self-hosted Plex picker that helps a group agree on a movie or TV show. Create a room, invite friends, swipe independently, and keep narrowing unanimous matches until one title wins.

<p align="center">
  <img src="assets/screenshots/room.png" alt="ScreenDeck demo room with Alice and Bob" width="1000" />
</p>

!!! tip "New to ScreenDeck?"
Start with [Deployment](deployment/index.md) to run ScreenDeck, then check [Configuration](configuration/index.md) for the settings you can customize.

## Documentation

<div class="grid cards" markdown>

- :material-cog-outline:{ .lg .middle } **Configuration**

  ***

  Environment variables, command-line flags, room lifetime, logging, and Plex overrides.

  [:octicons-arrow-right-24: Configure ScreenDeck](configuration/index.md)

- :material-server-outline:{ .lg .middle } **Deployment**

  ***

  Run ScreenDeck with Docker Compose or Kubernetes and keep its SQLite data persistent.

  [:octicons-arrow-right-24: Deployment guide](deployment/index.md)

- :material-shield-lock-outline:{ .lg .middle } **Security**

  ***

  Understand Plex credential storage, room sessions, backups, logs, and network exposure.

  [:octicons-arrow-right-24: Security notes](security/index.md)

- :material-code-braces:{ .lg .middle } **Development**

  ***

  Build, test, format, generate screenshots, and work on the documentation site locally.

  [:octicons-arrow-right-24: Development guide](development/index.md)

</div>
