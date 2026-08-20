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

func (s *Store) SavePlexAuth(ctx context.Context, state plex.AuthState) error {
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
	_, err = s.db.ExecContext(ctx, `INSERT INTO plex_auth
(id,client_id,key_id,private_key,user_token,token_expires_at,server_id,server_name,server_url,server_token,updated_at)
VALUES(1,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET
client_id=excluded.client_id,key_id=excluded.key_id,private_key=excluded.private_key,user_token=excluded.user_token,
token_expires_at=excluded.token_expires_at,server_id=excluded.server_id,server_name=excluded.server_name,
server_url=excluded.server_url,server_token=excluded.server_token,updated_at=excluded.updated_at`,
		state.ClientID, state.KeyID, privateKey, userToken, state.TokenExpiresAt.Unix(), state.ServerID,
		state.ServerName, state.ServerURL, serverToken, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("save Plex authentication: %w", err)
	}
	return nil
}

// LoadPlexAuth loads and decrypts persisted Plex authentication state.
func (s *Store) LoadPlexAuth(ctx context.Context) (plex.AuthState, error) {
	var state plex.AuthState
	var privateKey, userToken, serverToken []byte
	var expires int64
	err := s.db.QueryRowContext(ctx, `SELECT client_id,key_id,private_key,user_token,token_expires_at,server_id,server_name,server_url,server_token FROM plex_auth WHERE id=1`).
		Scan(&state.ClientID, &state.KeyID, &privateKey, &userToken, &expires, &state.ServerID, &state.ServerName, &state.ServerURL, &serverToken)
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
	if len(plainPrivateKey) != 0 && len(plainPrivateKey) != ed25519.PrivateKeySize {
		return state, errors.New("stored Plex private key has an invalid size")
	}
	plainUserToken, err := s.open(userToken)
	if err != nil {
		return state, fmt.Errorf("decrypt Plex user token: %w", err)
	}
	plainServerToken, err := s.open(serverToken)
	if err != nil {
		return state, fmt.Errorf("decrypt Plex server token: %w", err)
	}
	if len(plainPrivateKey) == ed25519.PrivateKeySize {
		state.Method = plex.AuthMethodJWT
		state.PrivateKey = ed25519.PrivateKey(plainPrivateKey)
	} else {
		state.Method = plex.AuthMethodLegacy
	}
	state.UserToken = string(plainUserToken)
	state.ServerToken = string(plainServerToken)
	state.TokenExpiresAt = time.Unix(expires, 0).UTC()
	return state, nil
}
