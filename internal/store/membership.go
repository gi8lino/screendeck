package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// RoomMembershipCredential links a room participant to a persistent browser identity.
type RoomMembershipCredential struct {
	// IdentityHash is the stored digest of the long-lived browser identity token.
	IdentityHash string
	// SessionToken is the participant token returned when the room membership is resumed.
	SessionToken string
}

// RoomMembership describes an active room associated with one browser identity.
type RoomMembership struct {
	// Code is the six-character room identifier.
	Code string `json:"code"`
	// Round is the current one-based room round.
	Round int `json:"round"`
	// Phase is the current room lifecycle phase.
	Phase RoomPhase `json:"phase"`
	// ParticipantID identifies the browser identity's participant in the room.
	ParticipantID string `json:"participantId"`
	// Name is the participant display name used in the room.
	Name string `json:"name"`
	// IsHost reports whether the participant currently owns the room.
	IsHost bool `json:"isHost"`
	// ParticipantCount is the number of active participants in the room.
	ParticipantCount int `json:"participantCount"`
	// CreatedAt is the room creation time.
	CreatedAt time.Time `json:"createdAt"`
	// ExpiresAt is the time at which the room becomes inactive.
	ExpiresAt time.Time `json:"expiresAt"`
}

// MembershipSession contains the participant credentials persisted for a browser identity.
type MembershipSession struct {
	// Code is the six-character room identifier.
	Code string
	// Token authenticates the participant to the room.
	Token string
}

// RoomMemberships returns active rooms associated with a browser identity.
func (s *Store) RoomMemberships(ctx context.Context, identityHash string) ([]RoomMembership, error) {
	if identityHash == "" || identityHash == "invalid" {
		return nil, errors.New("browser identity is required")
	}
	const query = `
SELECT
  r.code,
  r.round,
  r.phase,
  p.id,
  p.name,
  r.owner_id = p.id,
  (
    SELECT COUNT(*)
    FROM participants active
    WHERE active.room_code = r.code
  ),
  r.created_at,
  r.expires_at
FROM room_memberships membership
JOIN rooms r
  ON r.code = membership.room_code
JOIN participants p
  ON p.id = membership.participant_id
 AND p.room_code = membership.room_code
WHERE membership.identity_hash = ?
  AND r.expires_at > ?
ORDER BY membership.updated_at DESC, r.created_at DESC
`
	rows, err := s.db.QueryContext(ctx, query, identityHash, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close() // nolint:errcheck

	memberships := make([]RoomMembership, 0)
	for rows.Next() {
		var membership RoomMembership
		var createdAt int64
		var expiresAt int64
		if err := rows.Scan(
			&membership.Code,
			&membership.Round,
			&membership.Phase,
			&membership.ParticipantID,
			&membership.Name,
			&membership.IsHost,
			&membership.ParticipantCount,
			&createdAt,
			&expiresAt,
		); err != nil {
			return nil, err
		}
		membership.CreatedAt = time.Unix(createdAt, 0).UTC()
		membership.ExpiresAt = time.Unix(expiresAt, 0).UTC()
		memberships = append(memberships, membership)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return memberships, nil
}

// RoomMembershipSession restores room credentials associated with a browser identity.
func (s *Store) RoomMembershipSession(ctx context.Context, identityHash, code string) (MembershipSession, error) {
	if identityHash == "" || identityHash == "invalid" {
		return MembershipSession{}, errors.New("browser identity is required")
	}
	const query = `
SELECT
  membership.session_token
FROM room_memberships membership
JOIN rooms r
  ON r.code = membership.room_code
JOIN participants p
  ON p.id = membership.participant_id
 AND p.room_code = membership.room_code
WHERE membership.identity_hash = ?
  AND membership.room_code = ?
  AND r.expires_at > ?
`
	var encryptedToken []byte
	if err := s.db.QueryRowContext(ctx, query, identityHash, code, time.Now().Unix()).Scan(&encryptedToken); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return MembershipSession{}, ErrNotFound
		}
		return MembershipSession{}, err
	}
	token, err := s.open(encryptedToken)
	if err != nil {
		return MembershipSession{}, fmt.Errorf("decrypt room membership session: %w", err)
	}
	const touchMembershipQuery = `
UPDATE room_memberships
SET updated_at = ?
WHERE identity_hash = ?
  AND room_code = ?
`
	if _, err := s.db.ExecContext(ctx, touchMembershipQuery, time.Now().Unix(), identityHash, code); err != nil {
		return MembershipSession{}, err
	}
	return MembershipSession{Code: code, Token: string(token)}, nil
}

// SaveRoomMembership claims an existing participant session for a browser identity.
func (s *Store) SaveRoomMembership(
	ctx context.Context,
	code, participantID string,
	credential RoomMembershipCredential,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // nolint:errcheck

	const identityRoomQuery = `
SELECT participant_id
FROM room_memberships
WHERE identity_hash = ?
  AND room_code = ?
`
	var linkedParticipantID string
	err = tx.QueryRowContext(ctx, identityRoomQuery, credential.IdentityHash, code).Scan(&linkedParticipantID)
	if err == nil && linkedParticipantID != participantID {
		return ErrMembershipConflict
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	// Possession of the participant token proves ownership of this room session.
	// Moving the association lets a browser recover after its identity cookie is cleared.
	const releasePreviousIdentityQuery = `
DELETE FROM room_memberships
WHERE participant_id = ?
  AND identity_hash <> ?
`
	if _, err := tx.ExecContext(ctx, releasePreviousIdentityQuery, participantID, credential.IdentityHash); err != nil {
		return err
	}

	if err := s.saveRoomMembershipTx(ctx, tx, code, participantID, credential); err != nil {
		return err
	}
	return tx.Commit()
}

// saveRoomMembershipTx persists a browser identity association inside an existing transaction.
func (s *Store) saveRoomMembershipTx(
	ctx context.Context,
	tx *sql.Tx,
	code, participantID string,
	credential RoomMembershipCredential,
) error {
	if credential.IdentityHash == "" || credential.IdentityHash == "invalid" {
		return errors.New("browser identity is required")
	}
	if credential.SessionToken == "" {
		return errors.New("participant session token is required")
	}

	const existingRoomMembershipQuery = `
SELECT participant_id
FROM room_memberships
WHERE identity_hash = ?
  AND room_code = ?
`
	var existingParticipantID string
	err := tx.QueryRowContext(ctx, existingRoomMembershipQuery, credential.IdentityHash, code).Scan(&existingParticipantID)
	if err == nil && existingParticipantID != participantID {
		return ErrMembershipConflict
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	const existingParticipantMembershipQuery = `
SELECT identity_hash
FROM room_memberships
WHERE participant_id = ?
`
	var existingIdentityHash string
	err = tx.QueryRowContext(ctx, existingParticipantMembershipQuery, participantID).Scan(&existingIdentityHash)
	if err == nil && existingIdentityHash != credential.IdentityHash {
		return ErrMembershipConflict
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	const participantQuery = `
SELECT 1
FROM participants p
JOIN rooms r
  ON r.code = p.room_code
WHERE p.id = ?
  AND p.room_code = ?
  AND r.expires_at > ?
`
	var exists int
	if err := tx.QueryRowContext(ctx, participantQuery, participantID, code, time.Now().Unix()).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}

	encryptedToken, err := s.seal([]byte(credential.SessionToken))
	if err != nil {
		return fmt.Errorf("encrypt room membership session: %w", err)
	}
	if err := ensureIdentityTx(ctx, tx, credential.IdentityHash); err != nil {
		return err
	}

	now := time.Now().Unix()
	const saveMembershipQuery = `
INSERT INTO room_memberships (
  identity_hash,
  room_code,
  participant_id,
  session_token,
  created_at,
  updated_at
) VALUES (
  ?, ?, ?, ?, ?, ?
)
ON CONFLICT (identity_hash, room_code) DO UPDATE SET
  participant_id = excluded.participant_id,
  session_token = excluded.session_token,
  updated_at = excluded.updated_at
`
	if _, err := tx.ExecContext(
		ctx,
		saveMembershipQuery,
		credential.IdentityHash,
		code,
		participantID,
		encryptedToken,
		now,
		now,
	); err != nil {
		return err
	}
	return nil
}

// ensureIdentityTx creates the persistent browser identity row when it does not already exist.
func ensureIdentityTx(ctx context.Context, tx *sql.Tx, identityHash string) error {
	const query = `
INSERT INTO browser_identities (
  token_hash,
  created_at
) VALUES (
  ?, ?
)
ON CONFLICT (token_hash) DO NOTHING
`
	_, err := tx.ExecContext(ctx, query, identityHash, time.Now().Unix())
	return err
}

// saveOptionalRoomMembershipTx persists at most one optional browser identity association.
func (s *Store) saveOptionalRoomMembershipTx(
	ctx context.Context,
	tx *sql.Tx,
	code, participantID string,
	credentials []RoomMembershipCredential,
) error {
	if len(credentials) == 0 {
		return nil
	}
	if len(credentials) > 1 {
		return errors.New("only one browser identity may be linked to a room participant")
	}
	return s.saveRoomMembershipTx(ctx, tx, code, participantID, credentials[0])
}
