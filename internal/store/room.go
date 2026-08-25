package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gi8lino/screendeck/internal/media"
)

// RoomPhase identifies the current lifecycle phase of a room.
type RoomPhase string

const (
	// RoomPhaseSwiping indicates that participants can still vote in the current round.
	RoomPhaseSwiping RoomPhase = "swiping"
	// RoomPhaseNextRoundRequested indicates that at least one participant requested the next round.
	RoomPhaseNextRoundRequested RoomPhase = "next_round_requested"
	// RoomPhaseRoundComplete indicates that no eligible votes remain and the round has no single winner.
	RoomPhaseRoundComplete RoomPhase = "round_complete"
	// RoomPhaseFinished indicates that the room has converged on one winning item.
	RoomPhaseFinished RoomPhase = "finished"
	// posterLookaheadSize limits how many upcoming posters the browser preloads.
	posterLookaheadSize = 3
)

// Room contains persisted room metadata.
type Room struct {
	// Code is the six-character room identifier.
	Code string `json:"code"`
	// Round is the current one-based room round.
	Round int `json:"round"`
	// Phase is the current room lifecycle phase.
	Phase RoomPhase `json:"phase"`
	// OwnerID identifies the participant that owns the room.
	OwnerID string `json:"ownerId"`
	// CreatedAt is the room creation time.
	CreatedAt time.Time `json:"createdAt"`
	// ExpiresAt is the time at which the room becomes inactive.
	ExpiresAt time.Time `json:"expiresAt"`
	// Locked reports whether the room rejects new participants.
	Locked bool `json:"locked"`
}

// Participant contains public room participant state.
type Participant struct {
	// ID is the stable participant identifier.
	ID string `json:"id"`
	// Name is the participant display name.
	Name string `json:"name"`
	// Genres contains the participant's personal genre preferences.
	Genres []string `json:"genres"`
	// GenreMode controls whether selected personal genres match any or all values.
	GenreMode string `json:"genreMode"`
	// IsHost reports whether the participant currently owns the room.
	IsHost bool `json:"isHost"`
	// ReadyForNextRound reports whether the participant has agreed to advance.
	ReadyForNextRound bool `json:"readyForNextRound"`
}

// MoreTitlesState reports whether unused first-round titles can be added.
type MoreTitlesState struct {
	// Available is the number of unused first-round titles still available.
	Available int `json:"available"`
	// CanAdd reports whether the host can activate more first-round titles.
	CanAdd bool `json:"canAdd"`
}

// WinnerState contains the final winning item and its supporters.
type WinnerState struct {
	// Item is the winning media item.
	Item media.Item `json:"item"`
	// LikedBy contains participants whose likes support the winner.
	LikedBy []Participant `json:"likedBy"`
}

// RoomState contains the participant-specific view of a room.
type RoomState struct {
	// Room contains room metadata.
	Room Room `json:"room"`
	// Me is the authenticated participant.
	Me Participant `json:"me"`
	// Participants contains all active room participants.
	Participants []Participant `json:"participants"`
	// Candidate is the next eligible item for the authenticated participant.
	Candidate *media.Item `json:"candidate,omitempty"`
	// PosterLookahead contains upcoming item identifiers in participant deck order.
	PosterLookahead []string `json:"posterLookahead,omitempty"`
	// Matches contains items unanimously liked by active participants.
	Matches []media.Item `json:"matches"`
	// Winner contains final winner details when the room is finished.
	Winner *WinnerState `json:"winner,omitempty"`
	// Progress contains participant-specific swipe counts.
	Progress Progress `json:"progress"`
	// NextRound contains group readiness state.
	NextRound NextRoundState `json:"nextRound"`
	// RoundComplete reports whether the current round has no remaining eligible votes.
	RoundComplete bool `json:"roundComplete"`
	// MoreTitles describes unused first-round titles available to the host.
	MoreTitles MoreTitlesState `json:"moreTitles"`
}

// Progress reports swipe progress for the current participant and round.
type Progress struct {
	// Voted is the number of eligible titles already voted on by the participant.
	Voted int `json:"voted"`
	// Total is the number of titles eligible for the participant.
	Total int `json:"total"`
	// RoundTotal is the total number of titles active in the round.
	RoundTotal int `json:"roundTotal"`
	// FilteredOut is the number of active round titles excluded by personal genres.
	FilteredOut int `json:"filteredOut"`
}

// NextRoundState reports group consensus for advancing to the next round.
type NextRoundState struct {
	// Ready is the number of active participants ready to advance.
	Ready int `json:"ready"`
	// Required is the number of active participants whose readiness is required.
	Required int `json:"required"`
	// Available reports whether enough matches and participants exist to request another round.
	Available bool `json:"available"`
	// RequestedBy identifies the participant who initiated next-round consensus.
	RequestedBy *Participant `json:"requestedBy,omitempty"`
}

// CreateRoom persists a room, its owner, the active deck, and the original eligible item pool.
func (s *Store) CreateRoom(
	ctx context.Context,
	room Room,
	participant Participant,
	tokenHash string,
	itemIDs []string,
	poolIDs []string,
	memberships ...RoomMembershipCredential,
) error {
	normalizeRoomCreation(&room, &participant)
	participantGenres, err := encodeParticipantGenres(participant.Genres)
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // nolint:errcheck

	if err := insertRoomTx(ctx, tx, room); err != nil {
		return err
	}
	if err := insertParticipantTx(ctx, tx, room.Code, participant, tokenHash, participantGenres); err != nil {
		return err
	}
	if err := s.saveOptionalRoomMembershipTx(ctx, tx, room.Code, participant.ID, memberships); err != nil {
		return err
	}
	if err := insertRoomItemsTx(ctx, tx, room.Code, itemIDs, 0); err != nil {
		return err
	}
	if err := insertRoomPoolTx(ctx, tx, room.Code, poolIDs, itemIDs); err != nil {
		return err
	}
	if err := reconcileRoomPhaseTx(ctx, tx, room.Code); err != nil {
		return err
	}

	return tx.Commit()
}

// normalizeRoomCreation applies default room and participant values before persistence.
func normalizeRoomCreation(room *Room, participant *Participant) {
	if room.Round <= 0 {
		room.Round = 1
	}
	if room.Phase == "" {
		room.Phase = RoomPhaseSwiping
	}
	if room.OwnerID == "" {
		room.OwnerID = participant.ID
	}

	normalizeParticipant(participant)
}

// normalizeParticipant applies persistence defaults to participant state.
func normalizeParticipant(participant *Participant) {
	if participant.Genres == nil {
		participant.Genres = []string{}
	}
	if participant.GenreMode == "" {
		participant.GenreMode = "any"
	}
}

// encodeParticipantGenres serializes participant genres for SQLite JSON queries.
func encodeParticipantGenres(genres []string) (string, error) {
	encoded, err := json.Marshal(genres)
	if err != nil {
		return "", fmt.Errorf("encode participant genres: %w", err)
	}
	return string(encoded), nil
}

// insertRoomTx inserts room metadata inside an existing transaction.
func insertRoomTx(ctx context.Context, tx *sql.Tx, room Room) error {
	const query = `
INSERT INTO rooms (
  code,
  round,
  phase,
  owner_id,
  created_at,
  expires_at,
  locked
) VALUES (
  ?, ?, ?, ?, ?, ?, ?
)
`
	_, err := tx.ExecContext(
		ctx,
		query,
		room.Code,
		room.Round,
		room.Phase,
		room.OwnerID,
		room.CreatedAt.Unix(),
		room.ExpiresAt.Unix(),
		room.Locked,
	)
	return err
}

// insertParticipantTx inserts a participant inside an existing room transaction.
func insertParticipantTx(
	ctx context.Context,
	tx *sql.Tx,
	code string,
	participant Participant,
	tokenHash string,
	genres string,
) error {
	const query = `
INSERT INTO participants (
  id,
  room_code,
  name,
  genres,
  genre_mode,
  token_hash,
  joined_at
) VALUES (
  ?, ?, ?, ?, ?, ?, ?
)
`
	_, err := tx.ExecContext(
		ctx,
		query,
		participant.ID,
		code,
		participant.Name,
		genres,
		participant.GenreMode,
		tokenHash,
		time.Now().Unix(),
	)
	return err
}

// insertRoomItemsTx inserts an ordered set of active room items at the supplied start position.
func insertRoomItemsTx(
	ctx context.Context,
	tx *sql.Tx,
	code string,
	itemIDs []string,
	startPosition int,
) error {
	const query = `
INSERT INTO room_items (
  room_code,
  item_id,
  position
) VALUES (
  ?, ?, ?
)
`
	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return err
	}
	defer stmt.Close() // nolint:errcheck

	for offset, itemID := range itemIDs {
		if _, err := stmt.ExecContext(ctx, code, itemID, startPosition+offset); err != nil {
			return err
		}
	}
	return nil
}

// insertRoomPoolTx inserts the original eligible item pool and marks active items as used.
func insertRoomPoolTx(
	ctx context.Context,
	tx *sql.Tx,
	code string,
	poolIDs []string,
	activeIDs []string,
) error {
	const query = `
INSERT INTO room_item_pool (
  room_code,
  item_id,
  position,
  used
) VALUES (
  ?, ?, ?, ?
)
`
	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return err
	}
	defer stmt.Close() // nolint:errcheck

	active := make(map[string]struct{}, len(activeIDs))
	for _, itemID := range activeIDs {
		active[itemID] = struct{}{}
	}
	for position, itemID := range poolIDs {
		_, used := active[itemID]
		if _, err := stmt.ExecContext(ctx, code, itemID, position, used); err != nil {
			return err
		}
	}

	return nil
}

// JoinRoom persists a participant in an active room.
func (s *Store) JoinRoom(
	ctx context.Context,
	code string,
	participant Participant,
	tokenHash string,
	memberships ...RoomMembershipCredential,
) error {
	normalizeParticipant(&participant)
	genres, err := encodeParticipantGenres(participant.Genres)
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // nolint:errcheck

	if err := joinParticipantTx(ctx, tx, code, participant, tokenHash, genres); err != nil {
		return err
	}
	if err := s.saveOptionalRoomMembershipTx(ctx, tx, code, participant.ID, memberships); err != nil {
		return err
	}

	// Membership changed, so any pending next-round agreement must be renewed
	// by the new set of active participants.
	if err := cancelNextRoundRequestTx(ctx, tx, code); err != nil {
		return err
	}
	if err := invalidateNonUnanimousMatchesTx(ctx, tx, code); err != nil {
		return err
	}
	if err := reconcileRoomPhaseTx(ctx, tx, code); err != nil {
		return err
	}

	return tx.Commit()
}

// joinParticipantTx inserts a participant only when the target room is still active.
func joinParticipantTx(
	ctx context.Context,
	tx *sql.Tx,
	code string,
	participant Participant,
	tokenHash string,
	genres string,
) error {
	const query = `
INSERT INTO participants (
  id,
  room_code,
  name,
  genres,
  genre_mode,
  token_hash,
  joined_at
)
SELECT
  ?,
  code,
  ?,
  ?,
  ?,
  ?,
  ?
FROM rooms
WHERE code = ?
  AND expires_at > ?
  AND locked = 0
`
	result, err := tx.ExecContext(
		ctx,
		query,
		participant.ID,
		participant.Name,
		genres,
		participant.GenreMode,
		tokenHash,
		time.Now().Unix(),
		code,
		time.Now().Unix(),
	)
	if err != nil {
		return err
	}

	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return joinRoomUnavailable(ctx, tx, code)
	}
	return nil
}

// joinRoomUnavailable distinguishes a missing room from one closed to new participants.
func joinRoomUnavailable(ctx context.Context, tx *sql.Tx, code string) error {
	const query = `
SELECT locked
FROM rooms
WHERE code = ?
  AND expires_at > ?
`
	var locked bool
	if err := tx.QueryRowContext(ctx, query, code, time.Now().Unix()).Scan(&locked); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if locked {
		return ErrRoomLocked
	}
	return ErrNotFound
}

// invalidateNonUnanimousMatchesTx removes matches invalidated by a newly joined participant.
func invalidateNonUnanimousMatchesTx(ctx context.Context, tx *sql.Tx, code string) error {
	const query = `
DELETE FROM item_matches
WHERE room_code = ?
  AND (
    SELECT COUNT(*)
    FROM item_votes
    WHERE room_code = ?
      AND item_id = item_matches.item_id
      AND liked = 1
  ) < (
    SELECT COUNT(*)
    FROM participants
    WHERE room_code = ?
  )
`
	_, err := tx.ExecContext(ctx, query, code, code, code)
	return err
}

// ParticipantByToken authenticates and returns a room participant.
func (s *Store) ParticipantByToken(ctx context.Context, code, tokenHash string) (Participant, error) {
	const query = `
SELECT
  id,
  name,
  genres,
  genre_mode
FROM participants
WHERE room_code = ?
  AND token_hash = ?
`
	var participant Participant
	var genres string
	err := s.db.QueryRowContext(ctx, query, code, tokenHash).Scan(
		&participant.ID,
		&participant.Name,
		&genres,
		&participant.GenreMode,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return participant, ErrNotFound
	}
	if err != nil {
		return participant, err
	}
	decodeGenres(genres, &participant.Genres)
	return participant, nil
}

// RoomGenres returns the genres represented by the current room deck.
func (s *Store) RoomGenres(ctx context.Context, code string) ([]string, error) {
	if err := s.ensureActiveRoom(ctx, code); err != nil {
		return nil, err
	}

	const query = `
SELECT DISTINCT CAST(j.value AS TEXT)
FROM (
  SELECT
    room_code,
    item_id
  FROM room_item_pool
  WHERE room_code = ?

  UNION

  SELECT
    room_code,
    item_id
  FROM room_items
  WHERE room_code = ?
) rp
JOIN media_items m
  ON m.id = rp.item_id
JOIN json_each(m.genres) j
WHERE trim(CAST(j.value AS TEXT)) <> ''
ORDER BY CAST(j.value AS TEXT) COLLATE NOCASE
`
	rows, err := s.db.QueryContext(ctx, query, code, code)
	if err != nil {
		return nil, err
	}
	defer rows.Close() // nolint:errcheck

	var genres []string
	for rows.Next() {
		var genre string
		if err := rows.Scan(&genre); err != nil {
			return nil, err
		}
		genres = append(genres, genre)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return genres, nil
}

// ensureActiveRoom verifies that a room exists and has not expired.
func (s *Store) ensureActiveRoom(ctx context.Context, code string) error {
	const query = `
SELECT 1
FROM rooms
WHERE code = ?
  AND expires_at > ?
`
	var exists int
	if err := s.db.QueryRowContext(ctx, query, code, time.Now().Unix()).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

// RoomState returns the state of a room for one participant.
func (s *Store) RoomState(ctx context.Context, code, participantID string) (RoomState, error) {
	room, err := s.loadRoom(ctx, code)
	if err != nil {
		return RoomState{}, err
	}

	me, err := s.loadRoomParticipant(ctx, code, participantID, room)
	if err != nil {
		return RoomState{}, err
	}
	participants, err := s.loadRoomParticipants(ctx, code, room)
	if err != nil {
		return RoomState{}, err
	}

	upcoming, err := s.nextItems(ctx, code, participantID, posterLookaheadSize+1)
	if err != nil {
		return RoomState{}, err
	}
	var candidate *media.Item
	var posterLookahead []string
	if len(upcoming) > 0 {
		candidate = &upcoming[0]
		posterLookahead = make([]string, 0, len(upcoming)-1)
		for _, item := range upcoming[1:] {
			posterLookahead = append(posterLookahead, item.ID)
		}
	}
	matches, err := s.matchItems(ctx, code)
	if err != nil {
		return RoomState{}, err
	}

	nextRound, err := s.loadNextRoundState(ctx, code, room, participants, matches)
	if err != nil {
		return RoomState{}, err
	}
	progress, err := s.loadProgress(ctx, code, participantID)
	if err != nil {
		return RoomState{}, err
	}

	remaining, err := s.roundRemaining(ctx, code)
	if err != nil {
		return RoomState{}, err
	}
	roundComplete := room.Phase == RoomPhaseRoundComplete || room.Phase == RoomPhaseFinished || remaining == 0

	winner, err := s.loadWinner(ctx, code, room, matches)
	if err != nil {
		return RoomState{}, err
	}
	moreTitles, err := s.loadMoreTitlesState(ctx, code, room.Round)
	if err != nil {
		return RoomState{}, err
	}

	return RoomState{
		Room:            room,
		Me:              me,
		Participants:    participants,
		Candidate:       candidate,
		PosterLookahead: posterLookahead,
		Matches:         matches,
		Winner:          winner,
		Progress:        progress,
		NextRound:       nextRound,
		RoundComplete:   roundComplete,
		MoreTitles:      moreTitles,
	}, nil
}

// loadRoom returns active room metadata.
func (s *Store) loadRoom(ctx context.Context, code string) (Room, error) {
	const query = `
SELECT
  code,
  round,
  phase,
  owner_id,
  created_at,
  expires_at,
  locked
FROM rooms
WHERE code = ?
  AND expires_at > ?
`
	var room Room
	var created, expires int64
	err := s.db.QueryRowContext(ctx, query, code, time.Now().Unix()).Scan(
		&room.Code,
		&room.Round,
		&room.Phase,
		&room.OwnerID,
		&created,
		&expires,
		&room.Locked,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Room{}, ErrNotFound
	}
	if err != nil {
		return Room{}, err
	}

	room.CreatedAt = time.Unix(created, 0).UTC()
	room.ExpiresAt = time.Unix(expires, 0).UTC()
	return room, nil
}

// SetRoomLocked changes whether a room accepts new participants when requested by its host.
func (s *Store) SetRoomLocked(ctx context.Context, code, tokenHash string, locked bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // nolint:errcheck

	if _, err := authenticateRoomHostTx(ctx, tx, code, tokenHash); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE rooms SET locked = ? WHERE code = ?", locked, code); err != nil {
		return err
	}
	return tx.Commit()
}

// loadRoomParticipant returns one participant with current-round readiness and host state.
func (s *Store) loadRoomParticipant(
	ctx context.Context,
	code string,
	participantID string,
	room Room,
) (Participant, error) {
	const query = `
SELECT
  p.id,
  p.name,
  p.genres,
  p.genre_mode,
  EXISTS (
    SELECT 1
    FROM round_ready rr
    WHERE rr.room_code = p.room_code
      AND rr.round = ?
      AND rr.participant_id = p.id
  )
FROM participants p
WHERE p.id = ?
  AND p.room_code = ?
`
	var participant Participant
	var genres string
	if err := s.db.QueryRowContext(ctx, query, room.Round, participantID, code).Scan(
		&participant.ID,
		&participant.Name,
		&genres,
		&participant.GenreMode,
		&participant.ReadyForNextRound,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Participant{}, ErrNotFound
		}
		return Participant{}, err
	}

	decodeGenres(genres, &participant.Genres)
	participant.IsHost = participant.ID == room.OwnerID
	return participant, nil
}

// loadRoomParticipants returns all participants with current-round readiness and host state.
func (s *Store) loadRoomParticipants(
	ctx context.Context,
	code string,
	room Room,
) ([]Participant, error) {
	const query = `
SELECT
  p.id,
  p.name,
  p.genres,
  p.genre_mode,
  EXISTS (
    SELECT 1
    FROM round_ready rr
    WHERE rr.room_code = p.room_code
      AND rr.round = ?
      AND rr.participant_id = p.id
  )
FROM participants p
WHERE p.room_code = ?
ORDER BY p.joined_at
`
	rows, err := s.db.QueryContext(ctx, query, room.Round, code)
	if err != nil {
		return nil, err
	}
	defer rows.Close() // nolint:errcheck

	var participants []Participant
	for rows.Next() {
		participant, err := scanParticipant(rows, room.OwnerID)
		if err != nil {
			return nil, err
		}
		participants = append(participants, participant)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return participants, nil
}

// scanParticipant decodes a participant row that includes current-round readiness.
func scanParticipant(row scanner, ownerID string) (Participant, error) {
	var participant Participant
	var genres string
	if err := row.Scan(
		&participant.ID,
		&participant.Name,
		&genres,
		&participant.GenreMode,
		&participant.ReadyForNextRound,
	); err != nil {
		return Participant{}, err
	}

	decodeGenres(genres, &participant.Genres)
	participant.IsHost = participant.ID == ownerID
	return participant, nil
}

// loadNextRoundState returns current-round readiness and requester information.
func (s *Store) loadNextRoundState(
	ctx context.Context,
	code string,
	room Room,
	participants []Participant,
	matches []media.Item,
) (NextRoundState, error) {
	state := NextRoundState{Required: len(participants)}

	const readyQuery = `
SELECT COUNT(*)
FROM round_ready rr
JOIN participants p
  ON p.id = rr.participant_id
 AND p.room_code = rr.room_code
WHERE rr.room_code = ?
  AND rr.round = ?
`
	if err := s.db.QueryRowContext(ctx, readyQuery, code, room.Round).Scan(&state.Ready); err != nil {
		return NextRoundState{}, err
	}

	requester, err := s.loadNextRoundRequester(ctx, code, room.OwnerID)
	if err != nil {
		return NextRoundState{}, err
	}
	state.RequestedBy = requester
	state.Available = state.Required > 1 && len(matches) > 1
	return state, nil
}

// loadNextRoundRequester returns the participant that initiated next-round consensus when still present.
func (s *Store) loadNextRoundRequester(
	ctx context.Context,
	code string,
	ownerID string,
) (*Participant, error) {
	const requesterIDQuery = `
SELECT next_round_requester_id
FROM rooms
WHERE code = ?
`
	var requesterID string
	if err := s.db.QueryRowContext(ctx, requesterIDQuery, code).Scan(&requesterID); err != nil {
		return nil, err
	}
	if requesterID == "" {
		return nil, nil
	}

	const requesterQuery = `
SELECT
  id,
  name,
  genre_mode
FROM participants
WHERE room_code = ?
  AND id = ?
`
	var requester Participant
	if err := s.db.QueryRowContext(ctx, requesterQuery, code, requesterID).Scan(
		&requester.ID,
		&requester.Name,
		&requester.GenreMode,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	requester.IsHost = requester.ID == ownerID
	return &requester, nil
}

// loadProgress returns participant-specific swipe progress for the active round.
func (s *Store) loadProgress(ctx context.Context, code, participantID string) (Progress, error) {
	const progressQuery = `
SELECT
  COUNT(v.item_id),
  COUNT(rm.item_id)
FROM participants p
JOIN room_items rm
  ON rm.room_code = p.room_code
JOIN media_items m
  ON m.id = rm.item_id
LEFT JOIN item_votes v
  ON v.room_code = rm.room_code
 AND v.item_id = rm.item_id
 AND v.participant_id = p.id
WHERE p.id = ?
  AND p.room_code = ?
  AND (
    json_array_length(p.genres) = 0
    OR (
      p.genre_mode = 'all'
      AND NOT EXISTS (
        SELECT 1
        FROM json_each(p.genres) pg
        WHERE NOT EXISTS (
          SELECT 1
          FROM json_each(m.genres) mg
          WHERE lower(trim(CAST(mg.value AS TEXT))) = lower(trim(CAST(pg.value AS TEXT)))
        )
      )
    )
    OR (
      p.genre_mode <> 'all'
      AND EXISTS (
        SELECT 1
        FROM json_each(m.genres) mg
        JOIN json_each(p.genres) pg
          ON lower(trim(CAST(mg.value AS TEXT))) = lower(trim(CAST(pg.value AS TEXT)))
      )
    )
  )
`
	var progress Progress
	if err := s.db.QueryRowContext(ctx, progressQuery, participantID, code).Scan(
		&progress.Voted,
		&progress.Total,
	); err != nil {
		return Progress{}, err
	}

	const roundTotalQuery = `
SELECT COUNT(*)
FROM room_items
WHERE room_code = ?
`
	if err := s.db.QueryRowContext(ctx, roundTotalQuery, code).Scan(&progress.RoundTotal); err != nil {
		return Progress{}, err
	}
	progress.FilteredOut = progress.RoundTotal - progress.Total
	return progress, nil
}

// loadWinner returns final winner details when the room has converged on one match.
func (s *Store) loadWinner(
	ctx context.Context,
	code string,
	room Room,
	matches []media.Item,
) (*WinnerState, error) {
	if room.Phase != RoomPhaseFinished || len(matches) != 1 {
		return nil, nil
	}

	const query = `
SELECT
  p.id,
  p.name,
  p.genres,
  p.genre_mode
FROM participants p
JOIN item_votes v
  ON v.participant_id = p.id
 AND v.room_code = p.room_code
WHERE p.room_code = ?
  AND v.item_id = ?
  AND v.liked = 1
ORDER BY p.joined_at
`
	rows, err := s.db.QueryContext(ctx, query, code, matches[0].ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close() // nolint:errcheck

	winner := &WinnerState{Item: matches[0]}
	for rows.Next() {
		participant, err := scanWinnerSupporter(rows, room.OwnerID)
		if err != nil {
			return nil, err
		}
		winner.LikedBy = append(winner.LikedBy, participant)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return winner, nil
}

// scanWinnerSupporter decodes a participant row used by final winner details.
func scanWinnerSupporter(row scanner, ownerID string) (Participant, error) {
	var participant Participant
	var genres string
	if err := row.Scan(&participant.ID, &participant.Name, &genres, &participant.GenreMode); err != nil {
		return Participant{}, err
	}

	decodeGenres(genres, &participant.Genres)
	participant.IsHost = participant.ID == ownerID
	return participant, nil
}

// loadMoreTitlesState returns the unused first-round pool state for the room host.
func (s *Store) loadMoreTitlesState(
	ctx context.Context,
	code string,
	round int,
) (MoreTitlesState, error) {
	if round != 1 {
		return MoreTitlesState{}, nil
	}

	const query = `
SELECT COUNT(*)
FROM room_item_pool
WHERE room_code = ?
  AND used = 0
`
	var state MoreTitlesState
	if err := s.db.QueryRowContext(ctx, query, code).Scan(&state.Available); err != nil {
		return MoreTitlesState{}, err
	}
	state.CanAdd = state.Available > 0
	return state, nil
}

// nextItems returns a participant's next unvoted media items in deck order.
func (s *Store) nextItems(ctx context.Context, code, participantID string, limit int) ([]media.Item, error) {
	const query = `
SELECT
  m.id,
  m.library_key,
  m.media_type,
  m.guid,
  m.title,
  m.year,
  m.summary,
  m.duration,
  m.rating,
  m.genres,
  m.viewed,
  m.added_at
FROM participants p
JOIN room_items rm
  ON rm.room_code = p.room_code
JOIN media_items m
  ON m.id = rm.item_id
LEFT JOIN item_votes v
  ON v.room_code = rm.room_code
 AND v.item_id = rm.item_id
 AND v.participant_id = p.id
WHERE p.id = ?
  AND p.room_code = ?
  AND v.item_id IS NULL
  AND (
    json_array_length(p.genres) = 0
    OR (
      p.genre_mode = 'all'
      AND NOT EXISTS (
        SELECT 1
        FROM json_each(p.genres) pg
        WHERE NOT EXISTS (
          SELECT 1
          FROM json_each(m.genres) mg
          WHERE lower(trim(CAST(mg.value AS TEXT))) = lower(trim(CAST(pg.value AS TEXT)))
        )
      )
    )
    OR (
      p.genre_mode <> 'all'
      AND EXISTS (
        SELECT 1
        FROM json_each(m.genres) mg
        JOIN json_each(p.genres) pg
          ON lower(trim(CAST(mg.value AS TEXT))) = lower(trim(CAST(pg.value AS TEXT)))
      )
    )
  )
ORDER BY rm.position
LIMIT ?
`
	rows, err := s.db.QueryContext(ctx, query, participantID, code, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close() // nolint:errcheck

	items := make([]media.Item, 0, limit)
	for rows.Next() {
		item, scanErr := scanItem(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// matchItems returns media liked by every active participant.
func (s *Store) matchItems(ctx context.Context, code string) ([]media.Item, error) {
	const query = `
SELECT
  m.id,
  m.library_key,
  m.media_type,
  m.guid,
  m.title,
  m.year,
  m.summary,
  m.duration,
  m.rating,
  m.genres,
  m.viewed,
  m.added_at
FROM item_matches x
JOIN media_items m
  ON m.id = x.item_id
WHERE x.room_code = ?
ORDER BY x.matched_at DESC, m.title
`
	rows, err := s.db.QueryContext(ctx, query, code)
	if err != nil {
		return nil, err
	}
	defer rows.Close() // nolint:errcheck

	var items []media.Item
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

// scanner abstracts database row scanning for media items.
type scanner interface {
	Scan(...any) error
}

// scanItem decodes a media item from a database row.
func scanItem(row scanner) (*media.Item, error) {
	var item media.Item
	var genres string
	if err := row.Scan(
		&item.ID,
		&item.LibraryKey,
		&item.Type,
		&item.GUID,
		&item.Title,
		&item.Year,
		&item.Summary,
		&item.Duration,
		&item.Rating,
		&genres,
		&item.Viewed,
		&item.AddedAt,
	); err != nil {
		return nil, err
	}
	decodeGenres(genres, &item.Genres)
	return &item, nil
}

// decodeGenres decodes a stored JSON genre list and falls back to an empty list.
func decodeGenres(raw string, target *[]string) {
	if err := json.Unmarshal([]byte(raw), target); err != nil {
		*target = []string{}
		return
	}
	if *target == nil {
		*target = []string{}
	}
}

// unanimousMatch reports whether every active participant liked an item.
func unanimousMatch(participants, likes int) bool {
	return participants > 1 && likes == participants
}

// setMatchStateTx persists whether an item is currently a unanimous room match.
func setMatchStateTx(
	ctx context.Context,
	tx *sql.Tx,
	code string,
	itemID string,
	matched bool,
) error {
	if !matched {
		const deleteMatchQuery = `
DELETE FROM item_matches
WHERE room_code = ?
  AND item_id = ?
`
		_, err := tx.ExecContext(ctx, deleteMatchQuery, code, itemID)
		return err
	}

	const insertMatchQuery = `
INSERT OR IGNORE INTO item_matches (
  room_code,
  item_id,
  matched_at
) VALUES (
  ?, ?, ?
)
`
	_, err := tx.ExecContext(ctx, insertMatchQuery, code, itemID, time.Now().Unix())
	return err
}

// Vote persists a participant vote and reports whether it produces a unanimous match.
func (s *Store) Vote(
	ctx context.Context,
	code string,
	participantID string,
	itemID string,
	liked bool,
) (matched bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback() // nolint:errcheck

	if err := upsertVoteTx(ctx, tx, code, participantID, itemID, liked); err != nil {
		return false, err
	}

	participants, likes, err := voteMatchCountsTx(ctx, tx, code, itemID)
	if err != nil {
		return false, err
	}
	matched = unanimousMatch(participants, likes)
	if err := setMatchStateTx(ctx, tx, code, itemID, matched); err != nil {
		return false, err
	}

	if err := cancelNextRoundIfUnavailableTx(ctx, tx, code); err != nil {
		return false, err
	}
	if err := reconcileRoomPhaseTx(ctx, tx, code); err != nil {
		return false, err
	}

	return matched, tx.Commit()
}

// upsertVoteTx records a vote only when the participant can vote for the active room item.
func upsertVoteTx(
	ctx context.Context,
	tx *sql.Tx,
	code string,
	participantID string,
	itemID string,
	liked bool,
) error {
	const query = `
INSERT INTO item_votes (
  room_code,
  participant_id,
  item_id,
  liked,
  created_at
)
SELECT
  ?, ?, ?, ?, ?
WHERE EXISTS (
  SELECT 1
  FROM participants p
  JOIN media_items m
    ON m.id = ?
  WHERE p.id = ?
    AND p.room_code = ?
    AND (
      json_array_length(p.genres) = 0
      OR (
        p.genre_mode = 'all'
        AND NOT EXISTS (
          SELECT 1
          FROM json_each(p.genres) pg
          WHERE NOT EXISTS (
            SELECT 1
            FROM json_each(m.genres) mg
            WHERE lower(trim(CAST(mg.value AS TEXT))) = lower(trim(CAST(pg.value AS TEXT)))
          )
        )
      )
      OR (
        p.genre_mode <> 'all'
        AND EXISTS (
          SELECT 1
          FROM json_each(m.genres) mg
          JOIN json_each(p.genres) pg
            ON lower(trim(CAST(mg.value AS TEXT))) = lower(trim(CAST(pg.value AS TEXT)))
        )
      )
    )
)
AND EXISTS (
  SELECT 1
  FROM room_items
  WHERE room_code = ?
    AND item_id = ?
)
ON CONFLICT (room_code, participant_id, item_id) DO UPDATE SET
  liked = excluded.liked,
  created_at = excluded.created_at
`
	result, err := tx.ExecContext(
		ctx,
		query,
		code,
		participantID,
		itemID,
		liked,
		time.Now().Unix(),
		itemID,
		participantID,
		code,
		code,
		itemID,
	)
	if err != nil {
		return err
	}

	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

// voteMatchCountsTx returns active participant and positive-vote counts for an item.
func voteMatchCountsTx(
	ctx context.Context,
	tx *sql.Tx,
	code string,
	itemID string,
) (participants, likes int, err error) {
	const query = `
SELECT
  (
    SELECT COUNT(*)
    FROM participants
    WHERE room_code = ?
  ),
  (
    SELECT COUNT(*)
    FROM item_votes
    WHERE room_code = ?
      AND item_id = ?
      AND liked = 1
  )
`
	err = tx.QueryRowContext(ctx, query, code, code, itemID).Scan(&participants, &likes)
	return participants, likes, err
}

// RemoveParticipant removes a non-host participant when requested by the current room host.
func (s *Store) RemoveParticipant(
	ctx context.Context,
	code string,
	requesterTokenHash string,
	participantID string,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // nolint:errcheck

	requesterID, err := authenticateRoomHostTx(ctx, tx, code, requesterTokenHash)
	if err != nil {
		return err
	}
	if requesterID == participantID {
		return errors.New("the room host cannot remove themselves")
	}

	if err := removeParticipantTx(ctx, tx, code, participantID); err != nil {
		return err
	}
	return tx.Commit()
}

// authenticateRoomHostTx authenticates a room participant and verifies that they are the current host.
func authenticateRoomHostTx(
	ctx context.Context,
	tx *sql.Tx,
	code string,
	tokenHash string,
) (string, error) {
	const hostQuery = `
SELECT id
FROM participants p
JOIN rooms r
  ON r.code = p.room_code
WHERE p.room_code = ?
  AND p.token_hash = ?
  AND r.owner_id = p.id
`
	var requesterID string
	if err := tx.QueryRowContext(ctx, hostQuery, code, tokenHash).Scan(&requesterID); err == nil {
		return requesterID, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}

	authenticated, err := roomTokenAuthenticatedTx(ctx, tx, code, tokenHash)
	if err != nil {
		return "", err
	}
	if !authenticated {
		return "", ErrNotFound
	}
	return "", ErrForbidden
}

// roomTokenAuthenticatedTx reports whether a participant token belongs to the room.
func roomTokenAuthenticatedTx(
	ctx context.Context,
	tx *sql.Tx,
	code string,
	tokenHash string,
) (authenticated bool, err error) {
	const query = `
SELECT COUNT(*)
FROM participants
WHERE room_code = ?
  AND token_hash = ?
`
	var count int
	if err := tx.QueryRowContext(ctx, query, code, tokenHash).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

// LeaveRoom removes an authenticated participant and transfers room ownership when necessary.
func (s *Store) LeaveRoom(ctx context.Context, code, tokenHash string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // nolint:errcheck

	const participantQuery = `
SELECT id
FROM participants
WHERE room_code = ?
  AND token_hash = ?
`
	var leavingID string
	if err := tx.QueryRowContext(ctx, participantQuery, code, tokenHash).Scan(&leavingID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if err := removeParticipantTx(ctx, tx, code, leavingID); err != nil {
		return err
	}
	return tx.Commit()
}

// removeParticipantTx deletes one participant and reconciles ownership, matches, and room phase.
func removeParticipantTx(ctx context.Context, tx *sql.Tx, code, participantID string) error {
	if err := ensureParticipantExistsTx(ctx, tx, code, participantID); err != nil {
		return err
	}
	if err := deleteParticipantTx(ctx, tx, code, participantID); err != nil {
		return err
	}
	if err := transferRoomOwnershipTx(ctx, tx, code, participantID); err != nil {
		return err
	}
	if err := completeDepartureMatchesTx(ctx, tx, code); err != nil {
		return err
	}
	if err := cancelNextRoundRequestTx(ctx, tx, code); err != nil {
		return err
	}
	return reconcileRoomPhaseTx(ctx, tx, code)
}

// ensureParticipantExistsTx verifies that a participant still belongs to the room.
func ensureParticipantExistsTx(ctx context.Context, tx *sql.Tx, code, participantID string) error {
	const query = `
SELECT COUNT(*)
FROM participants
WHERE room_code = ?
  AND id = ?
`
	var count int
	if err := tx.QueryRowContext(ctx, query, code, participantID).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

// deleteParticipantTx removes a participant and cascades its dependent room records.
func deleteParticipantTx(ctx context.Context, tx *sql.Tx, code, participantID string) error {
	const query = `
DELETE FROM participants
WHERE room_code = ?
  AND id = ?
`
	_, err := tx.ExecContext(ctx, query, code, participantID)
	return err
}

// transferRoomOwnershipTx transfers ownership when the departing participant was the host.
func transferRoomOwnershipTx(ctx context.Context, tx *sql.Tx, code, participantID string) error {
	const query = `
UPDATE rooms
SET owner_id = COALESCE(
  (
    SELECT id
    FROM participants
    WHERE room_code = ?
    ORDER BY joined_at, id
    LIMIT 1
  ),
  ''
)
WHERE code = ?
  AND owner_id = ?
`
	_, err := tx.ExecContext(ctx, query, code, code, participantID)
	return err
}

// completeDepartureMatchesTx records likes that became unanimous after a participant left.
func completeDepartureMatchesTx(ctx context.Context, tx *sql.Tx, code string) error {
	// Completed matches are intentionally retained even if room membership later changes.
	const query = `
INSERT OR IGNORE INTO item_matches (
  room_code,
  item_id,
  matched_at
)
SELECT
  rm.room_code,
  rm.item_id,
  ?
FROM room_items rm
WHERE rm.room_code = ?
  AND (
    SELECT COUNT(*)
    FROM participants
    WHERE room_code = ?
  ) > 1
  AND (
    SELECT COUNT(*)
    FROM item_votes
    WHERE room_code = ?
      AND item_id = rm.item_id
      AND liked = 1
  ) = (
    SELECT COUNT(*)
    FROM participants
    WHERE room_code = ?
  )
`
	_, err := tx.ExecContext(ctx, query, time.Now().Unix(), code, code, code, code)
	return err
}

// DeleteExpired removes rooms whose expiration time has passed.
func (s *Store) DeleteExpired(ctx context.Context) error {
	const query = `
DELETE FROM rooms
WHERE expires_at <= ?
`
	_, err := s.db.ExecContext(ctx, query, time.Now().Unix())
	return err
}
