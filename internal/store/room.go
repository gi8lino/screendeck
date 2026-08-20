package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gi8lino/screendeck/internal/plex"
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
	Item plex.Item `json:"item"`
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
	Candidate *plex.Item `json:"candidate,omitempty"`
	// Matches contains items unanimously liked by active participants.
	Matches []plex.Item `json:"matches"`
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
func (s *Store) CreateRoom(ctx context.Context, room Room, participant Participant, tokenHash string, itemIDs, poolIDs []string, memberships ...RoomMembershipCredential) error {
	if room.Round <= 0 {
		room.Round = 1
	}
	if room.Phase == "" {
		room.Phase = RoomPhaseSwiping
	}
	if room.OwnerID == "" {
		room.OwnerID = participant.ID
	}
	if participant.Genres == nil {
		participant.Genres = []string{}
	}
	if participant.GenreMode == "" {
		participant.GenreMode = "any"
	}

	participantGenres, err := json.Marshal(participant.Genres)
	if err != nil {
		return fmt.Errorf("encode participant genres: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // nolint:errcheck

	const createRoomQuery = `
INSERT INTO rooms (
  code,
  round,
  phase,
  owner_id,
  created_at,
  expires_at
) VALUES (
  ?, ?, ?, ?, ?, ?
)
`
	if _, err := tx.ExecContext(
		ctx,
		createRoomQuery,
		room.Code,
		room.Round,
		room.Phase,
		room.OwnerID,
		room.CreatedAt.Unix(),
		room.ExpiresAt.Unix(),
	); err != nil {
		return err
	}

	const createParticipantQuery = `
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
	if _, err := tx.ExecContext(
		ctx,
		createParticipantQuery,
		participant.ID,
		room.Code,
		participant.Name,
		string(participantGenres),
		participant.GenreMode,
		tokenHash,
		time.Now().Unix(),
	); err != nil {
		return err
	}

	if err := s.saveOptionalRoomMembershipTx(ctx, tx, room.Code, participant.ID, memberships); err != nil {
		return err
	}

	const insertRoomItemQuery = `
INSERT INTO room_items (
  room_code,
  item_id,
  position
) VALUES (
  ?, ?, ?
)
`
	stmt, err := tx.PrepareContext(ctx, insertRoomItemQuery)
	if err != nil {
		return err
	}
	defer stmt.Close() // nolint:errcheck
	for position, itemID := range itemIDs {
		if _, err := stmt.ExecContext(ctx, room.Code, itemID, position); err != nil {
			return err
		}
	}

	const insertPoolItemQuery = `
INSERT INTO room_item_pool (
  room_code,
  item_id,
  position,
  used
) VALUES (
  ?, ?, ?, ?
)
`
	poolStmt, err := tx.PrepareContext(ctx, insertPoolItemQuery)
	if err != nil {
		return err
	}
	defer poolStmt.Close() // nolint:errcheck

	used := make(map[string]struct{}, len(itemIDs))
	for _, itemID := range itemIDs {
		used[itemID] = struct{}{}
	}
	for position, itemID := range poolIDs {
		_, active := used[itemID]
		if _, err := poolStmt.ExecContext(ctx, room.Code, itemID, position, active); err != nil {
			return err
		}
	}
	if err := reconcileRoomPhaseTx(ctx, tx, room.Code); err != nil {
		return err
	}
	return tx.Commit()
}

// JoinRoom persists a participant in an active room.
func (s *Store) JoinRoom(ctx context.Context, code string, participant Participant, tokenHash string, memberships ...RoomMembershipCredential) error {
	if participant.Genres == nil {
		participant.Genres = []string{}
	}
	if participant.GenreMode == "" {
		participant.GenreMode = "any"
	}

	genres, err := json.Marshal(participant.Genres)
	if err != nil {
		return fmt.Errorf("encode participant genres: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // nolint:errcheck

	const joinQuery = `
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
`
	result, err := tx.ExecContext(
		ctx,
		joinQuery,
		participant.ID,
		participant.Name,
		string(genres),
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
		return ErrNotFound
	}
	if err := s.saveOptionalRoomMembershipTx(ctx, tx, code, participant.ID, memberships); err != nil {
		return err
	}

	// Membership changed, so any pending next-round agreement must be renewed
	// by the new set of active participants.
	if err := cancelNextRoundRequestTx(ctx, tx, code); err != nil {
		return err
	}

	// A newly joined participant has not voted yet, so prior matches are no
	// longer unanimous until that participant also likes them.
	const invalidateMatchesQuery = `
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
	if _, err := tx.ExecContext(ctx, invalidateMatchesQuery, code, code, code); err != nil {
		return err
	}
	if err := reconcileRoomPhaseTx(ctx, tx, code); err != nil {
		return err
	}
	return tx.Commit()
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
	const roomQuery = `
SELECT 1
FROM rooms
WHERE code = ?
  AND expires_at > ?
`
	var exists int
	if err := s.db.QueryRowContext(ctx, roomQuery, code, time.Now().Unix()).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	const genresQuery = `
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
  ON m.rating_key = rp.item_id
JOIN json_each(m.genres) j
WHERE trim(CAST(j.value AS TEXT)) <> ''
ORDER BY CAST(j.value AS TEXT) COLLATE NOCASE
`
	rows, err := s.db.QueryContext(ctx, genresQuery, code, code)
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

// RoomState returns the state of a room for one participant.
func (s *Store) RoomState(ctx context.Context, code, participantID string) (RoomState, error) {
	const roomQuery = `
SELECT
  code,
  round,
  phase,
  owner_id,
  created_at,
  expires_at
FROM rooms
WHERE code = ?
  AND expires_at > ?
`
	var state RoomState
	var created, expires int64
	err := s.db.QueryRowContext(ctx, roomQuery, code, time.Now().Unix()).Scan(
		&state.Room.Code,
		&state.Room.Round,
		&state.Room.Phase,
		&state.Room.OwnerID,
		&created,
		&expires,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return state, ErrNotFound
	}
	if err != nil {
		return state, err
	}
	state.Room.CreatedAt = time.Unix(created, 0).UTC()
	state.Room.ExpiresAt = time.Unix(expires, 0).UTC()

	const meQuery = `
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
	var meGenres string
	if err := s.db.QueryRowContext(ctx, meQuery, state.Room.Round, participantID, code).Scan(
		&state.Me.ID,
		&state.Me.Name,
		&meGenres,
		&state.Me.GenreMode,
		&state.Me.ReadyForNextRound,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return state, ErrNotFound
		}
		return state, err
	}
	decodeGenres(meGenres, &state.Me.Genres)
	state.Me.IsHost = state.Me.ID == state.Room.OwnerID

	const participantsQuery = `
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
	rows, err := s.db.QueryContext(ctx, participantsQuery, state.Room.Round, code)
	if err != nil {
		return state, err
	}
	for rows.Next() {
		var participant Participant
		var genres string
		if err := rows.Scan(
			&participant.ID,
			&participant.Name,
			&genres,
			&participant.GenreMode,
			&participant.ReadyForNextRound,
		); err != nil {
			return state, err
		}
		decodeGenres(genres, &participant.Genres)
		participant.IsHost = participant.ID == state.Room.OwnerID
		state.Participants = append(state.Participants, participant)
	}
	if err := rows.Err(); err != nil {
		rows.Close() // nolint:errcheck
		return state, err
	}
	if err := rows.Close(); err != nil {
		return state, err
	}

	state.Candidate, err = s.nextItem(ctx, code, participantID)
	if err != nil {
		return state, err
	}
	state.Matches, err = s.matchItems(ctx, code)
	if err != nil {
		return state, err
	}

	state.NextRound.Required = len(state.Participants)
	const readyQuery = `
SELECT COUNT(*)
FROM round_ready rr
JOIN participants p
  ON p.id = rr.participant_id
 AND p.room_code = rr.room_code
WHERE rr.room_code = ?
  AND rr.round = ?
`
	if err := s.db.QueryRowContext(ctx, readyQuery, code, state.Room.Round).Scan(&state.NextRound.Ready); err != nil {
		return state, err
	}

	const requesterIDQuery = `
SELECT next_round_requester_id
FROM rooms
WHERE code = ?
`
	var requester Participant
	var requesterID string
	if err := s.db.QueryRowContext(ctx, requesterIDQuery, code).Scan(&requesterID); err != nil {
		return state, err
	}
	if requesterID != "" {
		const requesterQuery = `
SELECT
  id,
  name,
  genre_mode
FROM participants
WHERE room_code = ?
  AND id = ?
`
		if err := s.db.QueryRowContext(ctx, requesterQuery, code, requesterID).Scan(
			&requester.ID,
			&requester.Name,
			&requester.GenreMode,
		); err == nil {
			requester.IsHost = requester.ID == state.Room.OwnerID
			state.NextRound.RequestedBy = &requester
		} else if !errors.Is(err, sql.ErrNoRows) {
			return state, err
		}
	}
	state.NextRound.Available = state.NextRound.Required > 1 && len(state.Matches) > 1

	const progressQuery = `
SELECT
  COUNT(v.item_id),
  COUNT(rm.item_id)
FROM participants p
JOIN room_items rm
  ON rm.room_code = p.room_code
JOIN media_items m
  ON m.rating_key = rm.item_id
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
	if err := s.db.QueryRowContext(ctx, progressQuery, participantID, code).Scan(
		&state.Progress.Voted,
		&state.Progress.Total,
	); err != nil {
		return state, err
	}

	const roundTotalQuery = `
SELECT COUNT(*)
FROM room_items
WHERE room_code = ?
`
	if err := s.db.QueryRowContext(ctx, roundTotalQuery, code).Scan(&state.Progress.RoundTotal); err != nil {
		return state, err
	}
	state.Progress.FilteredOut = state.Progress.RoundTotal - state.Progress.Total

	remaining, err := s.roundRemaining(ctx, code)
	if err != nil {
		return state, err
	}
	state.RoundComplete = state.Room.Phase == RoomPhaseRoundComplete || state.Room.Phase == RoomPhaseFinished
	if remaining == 0 && !state.RoundComplete {
		state.RoundComplete = true
	}

	if state.Room.Phase == RoomPhaseFinished && len(state.Matches) == 1 {
		winner := &WinnerState{Item: state.Matches[0]}
		const supportersQuery = `
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
		rows, err := s.db.QueryContext(ctx, supportersQuery, code, state.Matches[0].RatingKey)
		if err != nil {
			return state, err
		}
		for rows.Next() {
			var participant Participant
			var genres string
			if err := rows.Scan(&participant.ID, &participant.Name, &genres, &participant.GenreMode); err != nil {
				return state, err
			}
			decodeGenres(genres, &participant.Genres)
			participant.IsHost = participant.ID == state.Room.OwnerID
			winner.LikedBy = append(winner.LikedBy, participant)
		}
		if err := rows.Err(); err != nil {
			rows.Close() // nolint:errcheck
			return state, err
		}
		if err := rows.Close(); err != nil {
			return state, err
		}
		state.Winner = winner
	}

	if state.Room.Round == 1 {
		const availableQuery = `
SELECT COUNT(*)
FROM room_item_pool
WHERE room_code = ?
  AND used = 0
`
		if err := s.db.QueryRowContext(ctx, availableQuery, code).Scan(&state.MoreTitles.Available); err != nil {
			return state, err
		}
		state.MoreTitles.CanAdd = state.MoreTitles.Available > 0
	}
	return state, nil
}

// nextItem returns the next unvoted media item for a participant.
func (s *Store) nextItem(ctx context.Context, code, participantID string) (*plex.Item, error) {
	const query = `
SELECT
  m.rating_key,
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
  ON m.rating_key = rm.item_id
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
LIMIT 1
`
	item, err := scanItem(s.db.QueryRowContext(ctx, query, participantID, code))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return item, err
}

// matchItems returns media liked by every active participant.
func (s *Store) matchItems(ctx context.Context, code string) ([]plex.Item, error) {
	const query = `
SELECT
  m.rating_key,
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
  ON m.rating_key = x.item_id
WHERE x.room_code = ?
ORDER BY x.matched_at DESC, m.title
`
	rows, err := s.db.QueryContext(ctx, query, code)
	if err != nil {
		return nil, err
	}
	defer rows.Close() // nolint:errcheck

	var items []plex.Item
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
func scanItem(row scanner) (*plex.Item, error) {
	var item plex.Item
	var genres string
	if err := row.Scan(
		&item.RatingKey,
		&item.Library,
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
func setMatchStateTx(ctx context.Context, tx *sql.Tx, code, itemID string, matched bool) error {
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
func (s *Store) Vote(ctx context.Context, code, participantID, itemID string, liked bool) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback() // nolint:errcheck

	const voteQuery = `
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
    ON m.rating_key = ?
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
		voteQuery,
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
		return false, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if count == 0 {
		return false, ErrNotFound
	}

	const matchCountsQuery = `
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
	var participants, likes int
	if err := tx.QueryRowContext(ctx, matchCountsQuery, code, code, itemID).Scan(&participants, &likes); err != nil {
		return false, err
	}
	matched := unanimousMatch(participants, likes)
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

// RemoveParticipant removes a non-host participant when requested by the current room host.
func (s *Store) RemoveParticipant(ctx context.Context, code, requesterTokenHash, participantID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // nolint:errcheck

	const requesterQuery = `
SELECT id
FROM participants p
JOIN rooms r
  ON r.code = p.room_code
WHERE p.room_code = ?
  AND p.token_hash = ?
  AND r.owner_id = p.id
`
	var requesterID string
	if err := tx.QueryRowContext(ctx, requesterQuery, code, requesterTokenHash).Scan(&requesterID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			const participantQuery = `
SELECT COUNT(*)
FROM participants
WHERE room_code = ?
  AND token_hash = ?
`
			var authenticated int
			if queryErr := tx.QueryRowContext(ctx, participantQuery, code, requesterTokenHash).Scan(&authenticated); queryErr != nil {
				return queryErr
			}
			if authenticated == 0 {
				return ErrNotFound
			}
			return ErrForbidden
		}
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
	const targetQuery = `
SELECT COUNT(*)
FROM participants
WHERE room_code = ?
  AND id = ?
`
	var participants int
	if err := tx.QueryRowContext(ctx, targetQuery, code, participantID).Scan(&participants); err != nil {
		return err
	}
	if participants == 0 {
		return ErrNotFound
	}

	const deleteParticipantQuery = `
DELETE FROM participants
WHERE room_code = ?
  AND id = ?
`
	if _, err := tx.ExecContext(ctx, deleteParticipantQuery, code, participantID); err != nil {
		return err
	}

	const transferOwnershipQuery = `
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
	if _, err := tx.ExecContext(ctx, transferOwnershipQuery, code, code, participantID); err != nil {
		return err
	}

	// A departure can make previously cast likes unanimous. Completed matches
	// are intentionally retained even if the room membership later changes.
	const completeMatchesQuery = `
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
	if _, err := tx.ExecContext(ctx, completeMatchesQuery, time.Now().Unix(), code, code, code, code); err != nil {
		return err
	}
	if err := cancelNextRoundRequestTx(ctx, tx, code); err != nil {
		return err
	}
	if err := reconcileRoomPhaseTx(ctx, tx, code); err != nil {
		return err
	}
	return nil
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
