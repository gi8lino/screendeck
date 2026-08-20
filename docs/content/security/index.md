# Security

## Plex credentials

Plex account/server tokens and the experimental Ed25519 private key are encrypted with AES-256-GCM before they are stored in SQLite.

The separate `auth.key` file is created with mode `0600`. Back up the database and authentication key together; encrypted credentials cannot be recovered if the key is lost.

Plex credentials are kept on the ScreenDeck server and are not sent to guest browsers. Temporary Plex setup tokens only authorize the server-selection flow.

## Room sessions

Room participants receive random session tokens. ScreenDeck stores SHA-256 token hashes in the participant table while the active browser session is kept in local storage.

Each browser profile also receives a long-lived, random identity cookie marked `HttpOnly` and `SameSite=Lax`. ScreenDeck stores only the identity-token hash in SQLite. Room memberships linked to that identity contain an encrypted copy of the participant session token, using the same local encryption key as Plex credentials. This powers the **Your rooms** list and lets the browser resume the same participant instead of creating duplicates.

The browser identity is not a ScreenDeck account and does not sync between devices or browser profiles. Clearing the identity cookie removes that browser's ability to discover its saved room memberships, although an already active room session can be claimed again while its participant token is still valid. Rooms expire according to `room-ttl`, which defaults to 24 hours. Leaving a room or being removed deletes the participant and its saved membership.

## Network exposure

The default deployment is intended for a trusted LAN or private network. For remote use, put ScreenDeck behind HTTPS and access control, or use a private mesh VPN.

## Logs

Diagnostic logging includes request IDs, Plex connection metadata, HTTP status, and request duration where useful. Plex tokens, PIN codes, JWTs, and private keys should never be written to logs.
