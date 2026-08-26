package store

import (
	"context"
	"time"
)

// DeleteExpired removes rooms whose expiration time has passed.
func (s *Store) DeleteExpired(ctx context.Context) error {
	const query = `
DELETE FROM rooms
WHERE expires_at <= ?
`
	_, err := s.db.ExecContext(ctx, query, time.Now().Unix())
	return err
}
