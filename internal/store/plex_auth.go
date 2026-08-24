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

var _ plex.AuthStore = (*Store)(nil) // Ensure Store implements Plex auth persistence.

// storedPlexAuth contains the encrypted values read from or written to SQLite.
type storedPlexAuth struct {
	// method identifies the Plex authorization flow.
	method string
	// privateKey contains the encrypted Ed25519 private key.
	privateKey []byte
	// userToken contains the encrypted Plex account token.
	userToken []byte
	// serverToken contains the encrypted Plex server token.
	serverToken []byte
	// expires contains the account-token expiry as a Unix timestamp.
	expires int64
}

// SavePlexAuth encrypts and persists Plex authentication state.
func (s *Store) SavePlexAuth(ctx context.Context, state plex.AuthState) error {
	if err := validatePlexAuthState(state); err != nil {
		return err
	}

	stored, err := s.encryptPlexAuth(state)
	if err != nil {
		return err
	}

	if err := s.savePlexAuth(ctx, state, stored); err != nil {
		return fmt.Errorf("save Plex authentication: %w", err)
	}

	return nil
}

// validatePlexAuthState verifies that authentication-specific key material is consistent.
func validatePlexAuthState(state plex.AuthState) error {
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

	return nil
}

// encryptPlexAuth encrypts the sensitive portions of Plex authentication state.
func (s *Store) encryptPlexAuth(state plex.AuthState) (storedPlexAuth, error) {
	privateKey, err := s.seal(state.PrivateKey)
	if err != nil {
		return storedPlexAuth{}, err
	}
	userToken, err := s.seal([]byte(state.UserToken))
	if err != nil {
		return storedPlexAuth{}, err
	}
	serverToken, err := s.seal([]byte(state.ServerToken))
	if err != nil {
		return storedPlexAuth{}, err
	}

	return storedPlexAuth{
		method:      string(state.Method),
		privateKey:  privateKey,
		userToken:   userToken,
		serverToken: serverToken,
		expires:     state.TokenExpiresAt.Unix(),
	}, nil
}

// savePlexAuth writes encrypted Plex authentication state to SQLite.
func (s *Store) savePlexAuth(ctx context.Context, state plex.AuthState, stored storedPlexAuth) error {
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
	_, err := s.db.ExecContext(
		ctx,
		query,
		stored.method,
		state.ClientID,
		state.KeyID,
		stored.privateKey,
		stored.userToken,
		stored.expires,
		state.ServerID,
		state.ServerName,
		state.ServerURL,
		stored.serverToken,
		time.Now().Unix(),
	)
	return err
}

// LoadPlexAuth loads and decrypts persisted Plex authentication state.
func (s *Store) LoadPlexAuth(ctx context.Context) (plex.AuthState, error) {
	state, stored, err := s.loadPlexAuth(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return state, plex.ErrAuthNotFound
	}
	if err != nil {
		return state, err
	}

	if err := s.decryptPlexAuth(&state, stored); err != nil {
		return state, err
	}

	return state, nil
}

// loadPlexAuth reads encrypted Plex authentication state from SQLite.
func (s *Store) loadPlexAuth(
	ctx context.Context,
) (state plex.AuthState, stored storedPlexAuth, err error) {
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
	err = s.db.QueryRowContext(ctx, query).Scan(
		&stored.method,
		&state.ClientID,
		&state.KeyID,
		&stored.privateKey,
		&stored.userToken,
		&stored.expires,
		&state.ServerID,
		&state.ServerName,
		&state.ServerURL,
		&stored.serverToken,
	)
	return state, stored, err
}

// decryptPlexAuth decrypts and validates the sensitive portions of stored Plex authentication state.
func (s *Store) decryptPlexAuth(state *plex.AuthState, stored storedPlexAuth) error {
	privateKey, err := s.open(stored.privateKey)
	if err != nil {
		return fmt.Errorf("decrypt Plex private key: %w", err)
	}
	userToken, err := s.open(stored.userToken)
	if err != nil {
		return fmt.Errorf("decrypt Plex user token: %w", err)
	}
	serverToken, err := s.open(stored.serverToken)
	if err != nil {
		return fmt.Errorf("decrypt Plex server token: %w", err)
	}

	state.Method = plex.AuthMethod(stored.method)
	if err := applyStoredPlexPrivateKey(state, privateKey); err != nil {
		return err
	}

	state.UserToken = string(userToken)
	state.ServerToken = string(serverToken)
	state.TokenExpiresAt = time.Unix(stored.expires, 0).UTC()
	return nil
}

// applyStoredPlexPrivateKey validates stored key material and assigns it for JWT authentication.
func applyStoredPlexPrivateKey(state *plex.AuthState, privateKey []byte) error {
	switch state.Method {
	case plex.AuthMethodStandard:
		if len(privateKey) != 0 {
			return errors.New("stored standard Plex authentication contains a private key")
		}
	case plex.AuthMethodJWT:
		if len(privateKey) != ed25519.PrivateKeySize {
			return errors.New("stored Plex private key has an invalid size")
		}
		state.PrivateKey = ed25519.PrivateKey(privateKey)
	default:
		return errors.New("stored Plex authentication method is invalid")
	}

	return nil
}
