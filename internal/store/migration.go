package store

import (
	"context"
	"fmt"
)

// migrate creates the canonical schema and copies data from pre-media-item tables when present.
func (s *Store) migrate(ctx context.Context) error {
	const schema = `
CREATE TABLE IF NOT EXISTS libraries (
  key TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  synced_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS media_items (
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

CREATE INDEX IF NOT EXISTS media_items_library_idx
  ON media_items (library_key);

CREATE TABLE IF NOT EXISTS rooms (
  code TEXT PRIMARY KEY,
  round INTEGER NOT NULL DEFAULT 1,
  phase TEXT NOT NULL DEFAULT 'swiping',
  next_round_requester_id TEXT NOT NULL DEFAULT '',
  owner_id TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS room_items (
  room_code TEXT NOT NULL REFERENCES rooms (code) ON DELETE CASCADE,
  item_id TEXT NOT NULL REFERENCES media_items (rating_key),
  position INTEGER NOT NULL,
  PRIMARY KEY (room_code, item_id)
);

CREATE INDEX IF NOT EXISTS room_items_order_idx
  ON room_items (room_code, position);

CREATE TABLE IF NOT EXISTS room_item_pool (
  room_code TEXT NOT NULL REFERENCES rooms (code) ON DELETE CASCADE,
  item_id TEXT NOT NULL REFERENCES media_items (rating_key),
  position INTEGER NOT NULL,
  used INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (room_code, item_id)
);

CREATE INDEX IF NOT EXISTS room_item_pool_order_idx
  ON room_item_pool (room_code, used, position);

CREATE TABLE IF NOT EXISTS participants (
  id TEXT PRIMARY KEY,
  room_code TEXT NOT NULL REFERENCES rooms (code) ON DELETE CASCADE,
  name TEXT NOT NULL,
  genres TEXT NOT NULL DEFAULT '[]',
  genre_mode TEXT NOT NULL DEFAULT 'any',
  token_hash TEXT NOT NULL UNIQUE,
  joined_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS participants_room_idx
  ON participants (room_code);

CREATE TABLE IF NOT EXISTS round_ready (
  room_code TEXT NOT NULL REFERENCES rooms (code) ON DELETE CASCADE,
  round INTEGER NOT NULL,
  participant_id TEXT NOT NULL REFERENCES participants (id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (room_code, round, participant_id)
);

CREATE TABLE IF NOT EXISTS item_votes (
  room_code TEXT NOT NULL REFERENCES rooms (code) ON DELETE CASCADE,
  participant_id TEXT NOT NULL REFERENCES participants (id) ON DELETE CASCADE,
  item_id TEXT NOT NULL REFERENCES media_items (rating_key),
  liked INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (room_code, participant_id, item_id)
);

CREATE TABLE IF NOT EXISTS item_matches (
  room_code TEXT NOT NULL REFERENCES rooms (code) ON DELETE CASCADE,
  item_id TEXT NOT NULL REFERENCES media_items (rating_key),
  matched_at INTEGER NOT NULL,
  PRIMARY KEY (room_code, item_id)
);

CREATE TABLE IF NOT EXISTS plex_auth (
  id INTEGER PRIMARY KEY CHECK (id = 1),
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
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}
	if err := s.ensureColumn(ctx, "rooms", "round", "INTEGER NOT NULL DEFAULT 1"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "rooms", "phase", "TEXT NOT NULL DEFAULT 'swiping'"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "rooms", "next_round_requester_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "rooms", "owner_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "participants", "genres", "TEXT NOT NULL DEFAULT '[]'"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "participants", "genre_mode", "TEXT NOT NULL DEFAULT 'any'"); err != nil {
		return err
	}
	if err := s.migrateLegacyMediaSchema(ctx); err != nil {
		return err
	}

	const backfillOwnersQuery = `
UPDATE rooms
SET owner_id = COALESCE(
  (
    SELECT id
    FROM participants
    WHERE room_code = rooms.code
    ORDER BY joined_at, id
    LIMIT 1
  ),
  ''
)
WHERE owner_id = ''
`
	if _, err := s.db.ExecContext(ctx, backfillOwnersQuery); err != nil {
		return fmt.Errorf("backfill room owners: %w", err)
	}
	return nil
}

// migrateLegacyMediaSchema copies data from the original movie-named tables without keeping them active.
func (s *Store) migrateLegacyMediaSchema(ctx context.Context) error {
	exists, err := s.tableExists(ctx, "movies")
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if err := s.ensureColumn(ctx, "movies", "media_type", "TEXT NOT NULL DEFAULT 'movie'"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "movies", "added_at", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}

	const migrateItemsQuery = `
INSERT OR IGNORE INTO media_items (
  rating_key,
  library_key,
  media_type,
  guid,
  title,
  year,
  summary,
  duration,
  rating,
  thumb,
  genres,
  viewed,
  added_at
)
SELECT
  rating_key,
  library_key,
  media_type,
  guid,
  title,
  year,
  summary,
  duration,
  rating,
  thumb,
  genres,
  viewed,
  added_at
FROM movies
`
	if _, err := s.db.ExecContext(ctx, migrateItemsQuery); err != nil {
		return fmt.Errorf("migrate legacy media items: %w", err)
	}

	const migrateRoomItemsQuery = `
INSERT OR IGNORE INTO room_items (
  room_code,
  item_id,
  position
)
SELECT
  room_code,
  movie_id,
  position
FROM room_movies
`
	if err := s.copyLegacyTable(ctx, "room_movies", migrateRoomItemsQuery); err != nil {
		return err
	}

	const migrateRoomPoolQuery = `
INSERT OR IGNORE INTO room_item_pool (
  room_code,
  item_id,
  position,
  used
)
SELECT
  room_code,
  movie_id,
  position,
  used
FROM room_pool
`
	if err := s.copyLegacyTable(ctx, "room_pool", migrateRoomPoolQuery); err != nil {
		return err
	}

	const migrateVotesQuery = `
INSERT OR IGNORE INTO item_votes (
  room_code,
  participant_id,
  item_id,
  liked,
  created_at
)
SELECT
  room_code,
  participant_id,
  movie_id,
  liked,
  created_at
FROM votes
`
	if err := s.copyLegacyTable(ctx, "votes", migrateVotesQuery); err != nil {
		return err
	}

	const migrateMatchesQuery = `
INSERT OR IGNORE INTO item_matches (
  room_code,
  item_id,
  matched_at
)
SELECT
  room_code,
  movie_id,
  matched_at
FROM matches
`
	if err := s.copyLegacyTable(ctx, "matches", migrateMatchesQuery); err != nil {
		return err
	}

	// Remove the legacy tables after all copies succeed. Leaving them behind
	// would allow stale votes or matches to be copied back on a later start.
	if err := s.dropLegacyTable(ctx, "matches"); err != nil {
		return err
	}
	if err := s.dropLegacyTable(ctx, "votes"); err != nil {
		return err
	}
	if err := s.dropLegacyTable(ctx, "room_pool"); err != nil {
		return err
	}
	if err := s.dropLegacyTable(ctx, "room_movies"); err != nil {
		return err
	}
	if err := s.dropLegacyTable(ctx, "movies"); err != nil {
		return err
	}
	return nil
}

// copyLegacyTable runs a copy statement only when its source table exists.
func (s *Store) copyLegacyTable(ctx context.Context, table, statement string) error {
	exists, err := s.tableExists(ctx, table)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("migrate legacy %s: %w", table, err)
	}
	return nil
}

// dropLegacyTable removes a migrated legacy table when it exists.
func (s *Store) dropLegacyTable(ctx context.Context, table string) error {
	exists, err := s.tableExists(ctx, table)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	query := `DROP TABLE ` + table
	if _, err := s.db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("drop legacy %s: %w", table, err)
	}
	return nil
}

// tableExists reports whether a SQLite table is present.
func (s *Store) tableExists(ctx context.Context, table string) (bool, error) {
	const query = `
SELECT COUNT(*)
FROM sqlite_master
WHERE type = 'table'
  AND name = ?
`
	var count int
	if err := s.db.QueryRowContext(ctx, query, table).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

// ensureColumn adds a database column when it is absent.
func (s *Store) ensureColumn(ctx context.Context, table, column, definition string) error {
	query := `PRAGMA table_info(` + table + `)`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close() // nolint:errcheck

	found := false
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == column {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if found {
		return nil
	}

	query = `ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + definition
	if _, err := s.db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("add %s.%s: %w", table, column, err)
	}
	return nil
}
