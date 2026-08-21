package store

import (
	"context"
	"fmt"
)

// schemaVersion returns the SQLite schema version recorded for the database.
func (s *Store) schemaVersion(ctx context.Context) (int, error) {
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return 0, fmt.Errorf("read database schema version: %w", err)
	}
	return version, nil
}

// schemaEmpty reports whether the database contains no application tables.
func (s *Store) schemaEmpty(ctx context.Context) (bool, error) {
	const query = `
SELECT COUNT(*)
FROM sqlite_master
WHERE type = 'table'
  AND name NOT LIKE 'sqlite_%'
`
	var tableCount int
	if err := s.db.QueryRowContext(ctx, query).Scan(&tableCount); err != nil {
		return false, fmt.Errorf("inspect database schema: %w", err)
	}
	return tableCount == 0, nil
}
