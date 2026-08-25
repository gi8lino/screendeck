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
	if err := validateIdentityHash(identityHash); err != nil {
		return nil, err
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
		membership, err := scanRoomMembership(rows)
		if err != nil {
			return nil, err
		}
		memberships = append(memberships, membership)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return memberships, nil
}

// scanRoomMembership decodes one browser room membership from a database row.
func scanRoomMembership(row scanner) (RoomMembership, error) {
	var membership RoomMembership
	var createdAt, expiresAt int64
	if err := row.Scan(
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
		return RoomMembership{}, err
	}

	membership.CreatedAt = time.Unix(createdAt, 0).UTC()
	membership.ExpiresAt = time.Unix(expiresAt, 0).UTC()
	return membership, nil
}

// RoomMembershipSession restores room credentials associated with a browser identity.
func (s *Store) RoomMembershipSession(
	ctx context.Context,
	identityHash string,
	code string,
) (MembershipSession, error) {
	if err := validateIdentityHash(identityHash); err != nil {
		return MembershipSession{}, err
	}

	encryptedToken, err := s.roomMembershipToken(ctx, identityHash, code)
	if err != nil {
		return MembershipSession{}, err
	}

	token, err := s.open(encryptedToken)
	if err != nil {
		return MembershipSession{}, fmt.Errorf("decrypt room membership session: %w", err)
	}
	if err := s.touchRoomMembership(ctx, identityHash, code); err != nil {
		return MembershipSession{}, err
	}

	return MembershipSession{Code: code, Token: string(token)}, nil
}

// validateIdentityHash verifies that a browser identity digest can be used for persistence.
func validateIdentityHash(identityHash string) error {
	if identityHash == "" || identityHash == "invalid" {
		return errors.New("browser identity is required")
	}
	return nil
}

// roomMembershipToken returns the encrypted participant token for an active membership.
func (s *Store) roomMembershipToken(ctx context.Context, identityHash, code string) ([]byte, error) {
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
			return nil, ErrNotFound
		}
		return nil, err
	}
	return encryptedToken, nil
}

// touchRoomMembership records recent use of a browser room membership.
func (s *Store) touchRoomMembership(ctx context.Context, identityHash, code string) error {
	const query = `
UPDATE room_memberships
SET updated_at = ?
WHERE identity_hash = ?
  AND room_code = ?
`
	_, err := s.db.ExecContext(ctx, query, time.Now().Unix(), identityHash, code)
	return err
}

// saveRoomMembershipTx persists a browser identity association inside an existing transaction.
func (s *Store) saveRoomMembershipTx(
	ctx context.Context,
	tx *sql.Tx,
	code string,
	participantID string,
	credential RoomMembershipCredential,
) error {
	if err := validateRoomMembershipCredential(credential); err != nil {
		return err
	}
	if err := ensureRoomMembershipAvailableTx(ctx, tx, code, participantID, credential.IdentityHash); err != nil {
		return err
	}
	if err := ensureRoomParticipantTx(ctx, tx, code, participantID); err != nil {
		return err
	}

	encryptedToken, err := s.seal([]byte(credential.SessionToken))
	if err != nil {
		return fmt.Errorf("encrypt room membership session: %w", err)
	}
	if err := ensureIdentityTx(ctx, tx, credential.IdentityHash); err != nil {
		return err
	}

	return saveRoomMembershipRecordTx(ctx, tx, code, participantID, credential.IdentityHash, encryptedToken)
}

// validateRoomMembershipCredential verifies browser and participant membership credentials.
func validateRoomMembershipCredential(credential RoomMembershipCredential) error {
	if err := validateIdentityHash(credential.IdentityHash); err != nil {
		return err
	}
	if credential.SessionToken == "" {
		return errors.New("participant session token is required")
	}
	return nil
}

// ensureRoomMembershipAvailableTx verifies that neither side of a membership is linked elsewhere.
func ensureRoomMembershipAvailableTx(
	ctx context.Context,
	tx *sql.Tx,
	code string,
	participantID string,
	identityHash string,
) error {
	const identityQuery = `
SELECT participant_id
FROM room_memberships
WHERE identity_hash = ?
  AND room_code = ?
`
	var existingParticipantID string
	err := tx.QueryRowContext(ctx, identityQuery, identityHash, code).Scan(&existingParticipantID)
	if err == nil && existingParticipantID != participantID {
		return ErrMembershipConflict
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	const participantQuery = `
SELECT identity_hash
FROM room_memberships
WHERE participant_id = ?
`
	var existingIdentityHash string
	err = tx.QueryRowContext(ctx, participantQuery, participantID).Scan(&existingIdentityHash)
	if err == nil && existingIdentityHash != identityHash {
		return ErrMembershipConflict
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	return nil
}

// ensureRoomParticipantTx verifies that a participant belongs to an active room.
func ensureRoomParticipantTx(ctx context.Context, tx *sql.Tx, code, participantID string) error {
	const query = `
SELECT 1
FROM participants p
JOIN rooms r
  ON r.code = p.room_code
WHERE p.id = ?
  AND p.room_code = ?
  AND r.expires_at > ?
`
	var exists int
	if err := tx.QueryRowContext(ctx, query, participantID, code, time.Now().Unix()).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

// saveRoomMembershipRecordTx inserts or updates the encrypted browser membership record.
func saveRoomMembershipRecordTx(
	ctx context.Context,
	tx *sql.Tx,
	code string,
	participantID string,
	identityHash string,
	encryptedToken []byte,
) error {
	const query = `
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
	now := time.Now().Unix()
	_, err := tx.ExecContext(
		ctx,
		query,
		identityHash,
		code,
		participantID,
		encryptedToken,
		now,
		now,
	)
	return err
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
	code string,
	participantID string,
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
