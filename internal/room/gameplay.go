package room

import (
	"context"
	"strings"
)

// State returns a room state visible to an authenticated participant.
func (s *Service) State(
	ctx context.Context,
	code,
	token string,
) (State, error) {
	participant, err := s.store.ParticipantByToken(
		ctx,
		strings.ToUpper(code),
		hashToken(token),
	)
	if err != nil {
		return State{}, err
	}

	return s.store.RoomState(
		ctx,
		strings.ToUpper(code),
		participant.ID,
	)
}

// Vote records a participant vote and reports whether it produced a match.
func (s *Service) Vote(
	ctx context.Context,
	code,
	token,
	itemID string,
	liked bool,
) (matched bool, err error) {
	code = strings.ToUpper(code)

	participant, err := s.store.ParticipantByToken(
		ctx,
		code,
		hashToken(token),
	)
	if err != nil {
		return false, err
	}

	matched, err = s.store.Vote(
		ctx,
		code,
		participant.ID,
		itemID,
		liked,
	)
	if err == nil {
		s.Notify(code)
	}

	return matched, err
}

// AddMoreTitles expands the first round from its unused original eligible pool.
func (s *Service) AddMoreTitles(
	ctx context.Context,
	code,
	token string,
	count int,
) (MoreTitlesResult, error) {
	code = strings.ToUpper(strings.TrimSpace(code))

	participant, err := s.store.ParticipantByToken(
		ctx,
		code,
		hashToken(token),
	)
	if err != nil {
		return MoreTitlesResult{}, err
	}

	added, remaining, err := s.store.AddMoreTitles(
		ctx,
		code,
		participant.ID,
		count,
	)
	if err != nil {
		return MoreTitlesResult{}, err
	}

	s.Notify(code)

	return MoreTitlesResult{
		Added:     added,
		Remaining: remaining,
	}, nil
}

// SetNextRoundReady records whether a participant agrees to narrow the deck to current matches.
func (s *Service) SetNextRoundReady(
	ctx context.Context,
	code,
	token string,
	expectedRound int,
	ready bool,
) (RoundResult, error) {
	code = strings.ToUpper(strings.TrimSpace(code))

	participant, err := s.store.ParticipantByToken(
		ctx,
		code,
		hashToken(token),
	)
	if err != nil {
		return RoundResult{}, err
	}

	round,
		titles,
		readyCount,
		required,
		advanced,
		err := s.store.SetRoundReady(
		ctx,
		code,
		participant.ID,
		expectedRound,
		ready,
	)
	if err != nil {
		return RoundResult{}, err
	}

	s.Notify(code)

	return RoundResult{
		Round:    round,
		Titles:   titles,
		Ready:    readyCount,
		Required: required,
		Advanced: advanced,
	}, nil
}

// Leave removes a participant from a room.
func (s *Service) Leave(
	ctx context.Context,
	code,
	token string,
) error {
	code = strings.ToUpper(code)

	if err := s.store.LeaveRoom(
		ctx,
		code,
		hashToken(token),
	); err != nil {
		return err
	}

	s.Notify(code)

	return nil
}

// SetRoomLocked changes whether a room accepts new participants.
func (s *Service) SetRoomLocked(ctx context.Context, code, token string, locked bool) error {
	code = strings.ToUpper(strings.TrimSpace(code))
	if err := s.store.SetRoomLocked(ctx, code, hashToken(token), locked); err != nil {
		return err
	}
	s.Notify(code)
	return nil
}

// RemoveParticipant lets the current room host remove another participant from the room.
func (s *Service) RemoveParticipant(
	ctx context.Context,
	code,
	token,
	participantID string,
) error {
	code = strings.ToUpper(strings.TrimSpace(code))
	participantID = strings.TrimSpace(participantID)

	if participantID == "" {
		return InvalidInput("participant id is required")
	}

	if err := s.store.RemoveParticipant(
		ctx,
		code,
		hashToken(token),
		participantID,
	); err != nil {
		return err
	}

	s.Notify(code)

	return nil
}
