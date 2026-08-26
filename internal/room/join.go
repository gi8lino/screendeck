package room

import (
	"context"
	"errors"
	"strings"
)

// JoinForIdentity joins or resumes a room for a persistent browser identity.
func (s *Service) JoinForIdentity(
	ctx context.Context,
	code,
	name string,
	genres []string,
	genreMode GenreMode,
	identityToken string,
) (Session, error) {
	if strings.TrimSpace(identityToken) == "" {
		return Session{}, errors.New("browser identity is required")
	}

	normalizedCode := strings.ToUpper(strings.TrimSpace(code))

	session, err := s.ResumeIdentity(
		ctx,
		identityToken,
		normalizedCode,
	)
	if err == nil {
		return session, nil
	}

	if !errors.Is(err, ErrNotFound) {
		return Session{}, err
	}

	return s.join(
		ctx,
		normalizedCode,
		name,
		genres,
		genreMode,
		identityToken,
	)
}

// join adds a participant with a persistent browser identity association.
func (s *Service) join(
	ctx context.Context,
	code,
	name string,
	genres []string,
	genreMode GenreMode,
	identityToken string,
) (Session, error) {
	// Normalize the user-facing join input first.
	code, name, genreMode, err := normalizeJoinInput(
		code,
		name,
		genreMode,
	)
	if err != nil {
		return Session{}, err
	}

	// Resolve the participant's personal genres against the room deck.
	availableGenres, err := s.store.RoomGenres(ctx, code)
	if err != nil {
		return Session{}, err
	}

	participantGenres, err := canonicalGenres(
		genres,
		availableGenres,
	)
	if err != nil {
		return Session{}, err
	}

	// Create credentials and persist the participant membership.
	participantID, token, tokenHash, err := credentials()
	if err != nil {
		return Session{}, err
	}

	participant := Participant{
		ID:        participantID,
		Name:      name,
		Genres:    participantGenres,
		GenreMode: string(genreMode),
	}

	if err := s.store.JoinRoom(
		ctx,
		code,
		participant,
		tokenHash,
		membershipCredential(identityToken, token),
	); err != nil {
		return Session{}, err
	}

	s.Notify(code)

	return Session{
		Code:  code,
		Token: token,
	}, nil
}

// normalizeJoinInput validates and canonicalizes room join input.
func normalizeJoinInput(
	code,
	name string,
	genreMode GenreMode,
) (
	normalizedCode,
	normalizedName string,
	normalizedGenreMode GenreMode,
	err error,
) {
	normalizedCode = strings.ToUpper(strings.TrimSpace(code))
	normalizedName = cleanName(name)

	if len(normalizedCode) != 6 || normalizedName == "" {
		return "", "", "", errors.New(
			"a six-character room code and name are required",
		)
	}

	normalizedGenreMode = normalizeGenreMode(genreMode)

	if normalizedGenreMode == "" {
		return "", "", "", errors.New(
			"genre mode must be any or all",
		)
	}

	return normalizedCode, normalizedName, normalizedGenreMode, nil
}
