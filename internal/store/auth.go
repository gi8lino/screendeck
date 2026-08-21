package store

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/gi8lino/screendeck/internal/plex"
)

// SavePlexAuth encrypts and persists Plex authentication state.
func (s *Store) SavePlexAuth(ctx context.Context, state plex.AuthState) error {
	switch state.Method {
	case plex.AuthMethodStandard:
		if len(state.PrivateKey) != 0 {
			return errors.New("standard Plex authentication must not contain a private key")
		}
	case plex.AuthMethodJWT:
		if len(state.PrivateKey) != ed25519.PrivateKeySize {
			return errors.New("JWT Plex authentication requires a valid Ed25519 private key")
		}
	default:
		return errors.New("invalid Plex authentication method")
	}

	privateKey, err := s.seal(state.PrivateKey)
	if err != nil {
		return err
	}
	userToken, err := s.seal([]byte(state.UserToken))
	if err != nil {
		return err
	}
	serverToken, err := s.seal([]byte(state.ServerToken))
	if err != nil {
		return err
	}

	const query = `
INSERT INTO plex_auth (
  id,
  auth_method,
  client_id,
  key_id,
  private_key,
  user_token,
  token_expires_at,
  server_id,
  server_name,
  server_url,
  server_token,
  updated_at
) VALUES (
  1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
)
ON CONFLICT (id) DO UPDATE SET
  auth_method = excluded.auth_method,
  client_id = excluded.client_id,
  key_id = excluded.key_id,
  private_key = excluded.private_key,
  user_token = excluded.user_token,
  token_expires_at = excluded.token_expires_at,
  server_id = excluded.server_id,
  server_name = excluded.server_name,
  server_url = excluded.server_url,
  server_token = excluded.server_token,
  updated_at = excluded.updated_at
`
	_, err = s.db.ExecContext(
		ctx,
		query,
		state.Method,
		state.ClientID,
		state.KeyID,
		privateKey,
		userToken,
		state.TokenExpiresAt.Unix(),
		state.ServerID,
		state.ServerName,
		state.ServerURL,
		serverToken,
		time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("save Plex authentication: %w", err)
	}
	return nil
}

// LoadPlexAuth loads and decrypts persisted Plex authentication state.
func (s *Store) LoadPlexAuth(ctx context.Context) (plex.AuthState, error) {
	var state plex.AuthState
	var authMethod string
	var privateKey, userToken, serverToken []byte
	var expires int64

	const query = `
SELECT
  auth_method,
  client_id,
  key_id,
  private_key,
  user_token,
  token_expires_at,
  server_id,
  server_name,
  server_url,
  server_token
FROM plex_auth
WHERE id = 1
`
	err := s.db.QueryRowContext(ctx, query).Scan(
		&authMethod,
		&state.ClientID,
		&state.KeyID,
		&privateKey,
		&userToken,
		&expires,
		&state.ServerID,
		&state.ServerName,
		&state.ServerURL,
		&serverToken,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return state, plex.ErrAuthNotFound
	}
	if err != nil {
		return state, err
	}

	plainPrivateKey, err := s.open(privateKey)
	if err != nil {
		return state, fmt.Errorf("decrypt Plex private key: %w", err)
	}
	plainUserToken, err := s.open(userToken)
	if err != nil {
		return state, fmt.Errorf("decrypt Plex user token: %w", err)
	}
	plainServerToken, err := s.open(serverToken)
	if err != nil {
		return state, fmt.Errorf("decrypt Plex server token: %w", err)
	}

	state.Method = plex.AuthMethod(authMethod)
	switch state.Method {
	case plex.AuthMethodStandard:
		if len(plainPrivateKey) != 0 {
			return state, errors.New("stored standard Plex authentication contains a private key")
		}
	case plex.AuthMethodJWT:
		if len(plainPrivateKey) != ed25519.PrivateKeySize {
			return state, errors.New("stored Plex private key has an invalid size")
		}
		state.PrivateKey = ed25519.PrivateKey(plainPrivateKey)
	default:
		return state, errors.New("stored Plex authentication method is invalid")
	}

	state.UserToken = string(plainUserToken)
	state.ServerToken = string(plainServerToken)
	state.TokenExpiresAt = time.Unix(expires, 0).UTC()
	return state, nil
}
