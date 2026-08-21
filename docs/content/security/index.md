---
hide:
  - navigation
---

# Security

## Plex credentials

Plex account/server tokens and the experimental Ed25519 private key are encrypted with AES-256-GCM before they are stored in SQLite.

For persistent databases, the separate `auth.key` file is created with mode `0600`. Back up the database and authentication key together; encrypted credentials cannot be recovered if the key is lost.

When `database-path` is set to `:memory:`, ScreenDeck generates the encryption key in memory instead. No `auth.key` file is created, and both the key and database contents are lost when the application stops.

Plex credentials are kept on the ScreenDeck server and are not sent to guest browsers. Temporary Plex setup tokens only authorize the server-selection flow.

## Browser identities and saved rooms

Each browser profile receives a long-lived random identity cookie marked `HttpOnly` and `SameSite=Lax`. ScreenDeck stores only the hash of that identity token in SQLite.

When the browser creates or joins a room, the participant is associated with that browser identity. The saved membership contains an encrypted copy of the participant session token, using the same local encryption key as Plex credentials. This powers the **Your rooms** list and lets the browser resume the same participant instead of creating a duplicate.

The browser identity is not a ScreenDeck account. It is local to that browser profile and does not automatically synchronize between browsers, profiles, or devices. Clearing the identity cookie removes that browser's ability to discover its saved memberships through **Your rooms**.

## Room sessions

Room participants receive random session tokens. ScreenDeck stores SHA-256 token hashes in the participant table while the currently open browser room session is kept in local storage.

When a room is resumed from **Your rooms**, the backend uses the browser identity to recover the encrypted participant session token and returns the existing room session to that browser. Existing votes and host ownership therefore remain attached to the same participant.

Rooms expire according to `room-ttl`, which defaults to 24 hours. Leaving a room or being removed by the host deletes the participant and its saved membership. Expired rooms are removed by the normal room cleanup process and then disappear from **Your rooms**.

## Network exposure

The default deployment is intended for a trusted LAN or private network. For remote use, put ScreenDeck behind HTTPS and access control, or use a private mesh VPN.

## Logs

Diagnostic logging includes request IDs, Plex connection metadata, HTTP status, and request duration where useful. Plex tokens, browser identity tokens, participant session tokens, PIN codes, JWTs, and private keys should never be written to logs.
