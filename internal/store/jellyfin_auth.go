package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/gi8lino/screendeck/internal/jellyfin"
)

var _ jellyfin.AuthStore = (*Store)(nil) // Ensure Store implements Jellyfin auth persistence.

// SaveJellyfinAuth encrypts and persists Jellyfin authorization state.
func (s *Store) SaveJellyfinAuth(ctx context.Context, state jellyfin.AuthState) error {
	if state.ServerURL == "" || state.UserID == "" || state.AccessToken == "" || state.DeviceID == "" {
		return errors.New("invalid Jellyfin authentication state")
	}
	accessToken, err := s.seal([]byte(state.AccessToken))
	if err != nil {
		return err
	}
	const query = `
INSERT INTO jellyfin_auth (
  id,
  server_id,
  server_name,
  server_url,
  user_id,
  username,
  access_token,
  device_id,
  updated_at
) VALUES (
  1, ?, ?, ?, ?, ?, ?, ?, ?
)
ON CONFLICT (id) DO UPDATE SET
  server_id = excluded.server_id,
  server_name = excluded.server_name,
  server_url = excluded.server_url,
  user_id = excluded.user_id,
  username = excluded.username,
  access_token = excluded.access_token,
  device_id = excluded.device_id,
  updated_at = excluded.updated_at
`
	if _, err := s.db.ExecContext(
		ctx,
		query,
		state.ServerID,
		state.ServerName,
		state.ServerURL,
		state.UserID,
		state.Username,
		accessToken,
		state.DeviceID,
		time.Now().Unix(),
	); err != nil {
		return fmt.Errorf("save Jellyfin authentication: %w", err)
	}
	return nil
}

// LoadJellyfinAuth loads and decrypts persisted Jellyfin authorization state.
func (s *Store) LoadJellyfinAuth(ctx context.Context) (jellyfin.AuthState, error) {
	const query = `
SELECT
  server_id,
  server_name,
  server_url,
  user_id,
  username,
  access_token,
  device_id
FROM jellyfin_auth
WHERE id = 1
`
	var state jellyfin.AuthState
	var accessToken []byte
	if err := s.db.QueryRowContext(ctx, query).Scan(
		&state.ServerID,
		&state.ServerName,
		&state.ServerURL,
		&state.UserID,
		&state.Username,
		&accessToken,
		&state.DeviceID,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return jellyfin.AuthState{}, jellyfin.ErrAuthNotFound
		}
		return jellyfin.AuthState{}, err
	}
	plainToken, err := s.open(accessToken)
	if err != nil {
		return jellyfin.AuthState{}, fmt.Errorf("decrypt Jellyfin access token: %w", err)
	}
	state.AccessToken = string(plainToken)
	return state, nil
}
