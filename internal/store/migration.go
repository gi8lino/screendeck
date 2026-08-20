package store

import (
	"context"
	"fmt"
)

func (s *Store) migrate(ctx context.Context) error {
	const schema = `
CREATE TABLE IF NOT EXISTS libraries (
  key TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  synced_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS movies (
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
CREATE INDEX IF NOT EXISTS movies_library_idx ON movies(library_key);
CREATE TABLE IF NOT EXISTS rooms (
  code TEXT PRIMARY KEY,
  round INTEGER NOT NULL DEFAULT 1,
  phase TEXT NOT NULL DEFAULT 'swiping',
  next_round_requester_id TEXT NOT NULL DEFAULT '',
  owner_id TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS room_movies (
  room_code TEXT NOT NULL REFERENCES rooms(code) ON DELETE CASCADE,
  movie_id TEXT NOT NULL REFERENCES movies(rating_key),
  position INTEGER NOT NULL,
  PRIMARY KEY(room_code, movie_id)
);
CREATE INDEX IF NOT EXISTS room_movies_order_idx ON room_movies(room_code, position);
CREATE TABLE IF NOT EXISTS room_pool (
  room_code TEXT NOT NULL REFERENCES rooms(code) ON DELETE CASCADE,
  movie_id TEXT NOT NULL REFERENCES movies(rating_key),
  position INTEGER NOT NULL,
  used INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(room_code, movie_id)
);
CREATE INDEX IF NOT EXISTS room_pool_order_idx ON room_pool(room_code, used, position);
CREATE TABLE IF NOT EXISTS participants (
  id TEXT PRIMARY KEY,
  room_code TEXT NOT NULL REFERENCES rooms(code) ON DELETE CASCADE,
  name TEXT NOT NULL,
  genres TEXT NOT NULL DEFAULT '[]',
  genre_mode TEXT NOT NULL DEFAULT 'any',
  token_hash TEXT NOT NULL UNIQUE,
  joined_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS participants_room_idx ON participants(room_code);
CREATE TABLE IF NOT EXISTS round_ready (
  room_code TEXT NOT NULL REFERENCES rooms(code) ON DELETE CASCADE,
  round INTEGER NOT NULL,
  participant_id TEXT NOT NULL REFERENCES participants(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  PRIMARY KEY(room_code, round, participant_id)
);
CREATE TABLE IF NOT EXISTS votes (
  room_code TEXT NOT NULL REFERENCES rooms(code) ON DELETE CASCADE,
  participant_id TEXT NOT NULL REFERENCES participants(id) ON DELETE CASCADE,
  movie_id TEXT NOT NULL REFERENCES movies(rating_key),
  liked INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY(room_code, participant_id, movie_id)
);
CREATE TABLE IF NOT EXISTS matches (
  room_code TEXT NOT NULL REFERENCES rooms(code) ON DELETE CASCADE,
  movie_id TEXT NOT NULL REFERENCES movies(rating_key),
  matched_at INTEGER NOT NULL,
  PRIMARY KEY(room_code, movie_id)
);
CREATE TABLE IF NOT EXISTS plex_auth (
  id INTEGER PRIMARY KEY CHECK(id=1),
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
);`
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}
	if err := s.ensureColumn(ctx, "movies", "media_type", "TEXT NOT NULL DEFAULT 'movie'"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "movies", "added_at", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
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
	if _, err := s.db.ExecContext(ctx, `UPDATE rooms SET owner_id=COALESCE((SELECT id FROM participants WHERE room_code=rooms.code ORDER BY joined_at,id LIMIT 1),'') WHERE owner_id=''`); err != nil {
		return fmt.Errorf("backfill room owners: %w", err)
	}
	if err := s.ensureColumn(ctx, "participants", "genres", "TEXT NOT NULL DEFAULT '[]'"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "participants", "genre_mode", "TEXT NOT NULL DEFAULT 'any'"); err != nil {
		return err
	}
	return nil
}

// ensureColumn adds a database column when it is absent.
func (s *Store) ensureColumn(ctx context.Context, table, column, definition string) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return err
		}
		if name == column {
			found = true
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if found {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+column+` `+definition); err != nil {
		return fmt.Errorf("add %s.%s: %w", table, column, err)
	}
	return nil
}
