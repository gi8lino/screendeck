ALTER TABLE media_items RENAME COLUMN rating_key TO id;

ALTER TABLE media_items RENAME COLUMN thumb TO poster;

CREATE TABLE media_provider (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  provider TEXT NOT NULL CHECK (provider IN ('plex', 'jellyfin')),
  updated_at INTEGER NOT NULL
);

INSERT INTO media_provider (
  id,
  provider,
  updated_at
)
SELECT
  1,
  'plex',
  CAST(strftime('%s', 'now') AS INTEGER)
WHERE EXISTS (
  SELECT 1
  FROM plex_auth
  WHERE id = 1
);

CREATE TABLE jellyfin_auth (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  server_id TEXT NOT NULL,
  server_name TEXT NOT NULL,
  server_url TEXT NOT NULL,
  user_id TEXT NOT NULL,
  username TEXT NOT NULL,
  access_token BLOB NOT NULL,
  device_id TEXT NOT NULL,
  updated_at INTEGER NOT NULL
);
