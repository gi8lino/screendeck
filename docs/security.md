# Security

## Plex credentials

Plex account/server tokens and the experimental Ed25519 private key are encrypted with AES-256-GCM before they are stored in SQLite.

The separate `auth.key` file is created with mode `0600`. Back up the database and authentication key together; encrypted credentials cannot be recovered if the key is lost.

Plex credentials are kept on the ScreenDeck server and are not sent to guest browsers. Temporary Plex setup tokens only authorize the server-selection flow.

## Room sessions

Room participants receive random session tokens. ScreenDeck stores SHA-256 token hashes in the database while the browser stores its own room token in local storage. Rooms expire according to `room-ttl`, which defaults to 24 hours.

## Network exposure

The default deployment is intended for a trusted LAN or private network. For remote use, put ScreenDeck behind HTTPS and access control, or use a private mesh VPN.

## Logs

Diagnostic logging includes request IDs, Plex connection metadata, HTTP status, and request duration where useful. Plex tokens, PIN codes, JWTs, and private keys should never be written to logs.
