package store

import (
	"context"
	"errors"
	"fmt"
)

const schemaVersion = 1

const schema = `
CREATE TABLE libraries (
  key TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  synced_at INTEGER NOT NULL
);

CREATE TABLE media_items (
  rating_key TEXT PRIMARY KEY,
  library_key TEXT NOT NULL,
  media_type TEXT NOT NULL DEFAULT 'movie',
  guid TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL,
  year INTEGER NOT NULL DEFAULT 0,
  summary TEXT NOT NULL DEFAULT '',
  duration INTEGER NOT NULL DEFAULT 0,
  rating REAL NOT NULL DEFAULT 0,
  thumb TEXT NOT NULL DEFAULT '',
  genres TEXT NOT NULL DEFAULT '[]',
  viewed INTEGER NOT NULL DEFAULT 0,
  added_at INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX media_items_library_idx
  ON media_items (library_key);

CREATE TABLE rooms (
  code TEXT PRIMARY KEY,
  round INTEGER NOT NULL DEFAULT 1,
  phase TEXT NOT NULL DEFAULT 'swiping',
  next_round_requester_id TEXT NOT NULL DEFAULT '',
  owner_id TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL
);

CREATE TABLE room_items (
  room_code TEXT NOT NULL REFERENCES rooms (code) ON DELETE CASCADE,
  item_id TEXT NOT NULL REFERENCES media_items (rating_key),
  position INTEGER NOT NULL,
  PRIMARY KEY (room_code, item_id)
);

CREATE INDEX room_items_order_idx
  ON room_items (room_code, position);

CREATE TABLE room_item_pool (
  room_code TEXT NOT NULL REFERENCES rooms (code) ON DELETE CASCADE,
  item_id TEXT NOT NULL REFERENCES media_items (rating_key),
  position INTEGER NOT NULL,
  used INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (room_code, item_id)
);

CREATE INDEX room_item_pool_order_idx
  ON room_item_pool (room_code, used, position);

CREATE TABLE participants (
  id TEXT PRIMARY KEY,
  room_code TEXT NOT NULL REFERENCES rooms (code) ON DELETE CASCADE,
  name TEXT NOT NULL,
  genres TEXT NOT NULL DEFAULT '[]',
  genre_mode TEXT NOT NULL DEFAULT 'any',
  token_hash TEXT NOT NULL UNIQUE,
  joined_at INTEGER NOT NULL
);

CREATE INDEX participants_room_idx
  ON participants (room_code);

CREATE TABLE browser_identities (
  token_hash TEXT PRIMARY KEY,
  created_at INTEGER NOT NULL
);

CREATE TABLE room_memberships (
  identity_hash TEXT NOT NULL REFERENCES browser_identities (token_hash) ON DELETE CASCADE,
  room_code TEXT NOT NULL REFERENCES rooms (code) ON DELETE CASCADE,
  participant_id TEXT NOT NULL UNIQUE REFERENCES participants (id) ON DELETE CASCADE,
  session_token BLOB NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  PRIMARY KEY (identity_hash, room_code)
);

CREATE INDEX room_memberships_identity_idx
  ON room_memberships (identity_hash, updated_at DESC);

CREATE TABLE round_ready (
  room_code TEXT NOT NULL REFERENCES rooms (code) ON DELETE CASCADE,
  round INTEGER NOT NULL,
  participant_id TEXT NOT NULL REFERENCES participants (id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (room_code, round, participant_id)
);

CREATE TABLE item_votes (
  room_code TEXT NOT NULL REFERENCES rooms (code) ON DELETE CASCADE,
  participant_id TEXT NOT NULL REFERENCES participants (id) ON DELETE CASCADE,
  item_id TEXT NOT NULL REFERENCES media_items (rating_key),
  liked INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (room_code, participant_id, item_id)
);

CREATE TABLE item_matches (
  room_code TEXT NOT NULL REFERENCES rooms (code) ON DELETE CASCADE,
  item_id TEXT NOT NULL REFERENCES media_items (rating_key),
  matched_at INTEGER NOT NULL,
  PRIMARY KEY (room_code, item_id)
);

CREATE TABLE plex_auth (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  auth_method TEXT NOT NULL,
  client_id TEXT NOT NULL,
  key_id TEXT NOT NULL,
  private_key BLOB NOT NULL,
  user_token BLOB NOT NULL,
  token_expires_at INTEGER NOT NULL,
  server_id TEXT NOT NULL,
  server_name TEXT NOT NULL,
  server_url TEXT NOT NULL,
  server_token BLOB NOT NULL,
  updated_at INTEGER NOT NULL
);
`

// initSchema creates the current schema for an empty database and rejects incompatible schemas.
func (s *Store) initSchema(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read database schema version: %w", err)
	}
	if version == schemaVersion {
		return nil
	}
	if version != 0 {
		return fmt.Errorf(
			"unsupported database schema version %d, expected %d; recreate the database",
			version,
			schemaVersion,
		)
	}

	const tableCountQuery = `
SELECT COUNT(*)
FROM sqlite_master
WHERE type = 'table'
  AND name NOT LIKE 'sqlite_%'
`
	var tableCount int
	if err := s.db.QueryRowContext(ctx, tableCountQuery).Scan(&tableCount); err != nil {
		return fmt.Errorf("inspect database schema: %w", err)
	}
	if tableCount != 0 {
		return errors.New("database schema is unversioned; recreate the database")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // nolint:errcheck

	if _, err := tx.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("create database schema: %w", err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		return fmt.Errorf("set database schema version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit database schema: %w", err)
	}
	return nil
}
