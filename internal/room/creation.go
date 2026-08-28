package room

import (
	"cmp"
	"context"
	"strings"
	"time"
)

// CreateOptions contains the inputs required to create a room.
type CreateOptions struct {
	Name          string
	LibraryKeys   []string
	Filters       Filters
	Genres        []string
	GenreMode     GenreMode
	Sampling      SamplingStrategy
	RoundSize     int
	LifetimeHours int
	IdentityToken string
}

// CreateForIdentity creates a room and associates the host with a persistent browser identity.
func (s *Service) CreateForIdentity(
	ctx context.Context,
	options CreateOptions,
) (Session, error) {
	if strings.TrimSpace(options.IdentityToken) == "" {
		return Session{}, InvalidInput("browser identity is required")
	}
	return s.create(ctx, options)
}

// create creates a room with a persistent browser identity association.
func (s *Service) create(
	ctx context.Context,
	options CreateOptions,
) (Session, error) {
	// Normalize and validate room input before touching the catalog or store.
	options, err := normalizeCreateRoomOptions(options)
	if err != nil {
		return Session{}, err
	}

	// Build the complete eligible pool, then apply the first-round limit.
	_, items, err := s.loadItems(ctx, options.LibraryKeys)
	if err != nil {
		return Session{}, err
	}

	eligible := filterEligibleItems(items, options.Filters)
	if len(eligible) == 0 {
		return Session{}, InvalidInput("the selected libraries contain no matching titles")
	}

	pool, err := orderInitialItems(eligible, options.Sampling)
	if err != nil {
		return Session{}, err
	}

	participantGenres, err := canonicalGenres(
		options.Genres,
		genresFromItems(pool),
	)
	if err != nil {
		return Session{}, err
	}

	selected := limitItems(pool, options.RoundSize)

	// Create the room and host credentials only after the candidate pool is valid.
	code, err := roomCode()
	if err != nil {
		return Session{}, err
	}

	participantID, token, tokenHash, err := credentials()
	if err != nil {
		return Session{}, err
	}

	// Persist both the active first-round deck and the full original pool.
	now := time.Now().UTC()

	if err := s.rooms.CreateRoom(
		ctx,
		Room{
			Code:      code,
			Round:     1,
			CreatedAt: now,
			ExpiresAt: now.Add(s.roomLifetime(options.LifetimeHours)),
		},
		Participant{
			ID:        participantID,
			Name:      options.Name,
			Genres:    participantGenres,
			GenreMode: string(options.GenreMode),
		},
		tokenHash,
		itemIDs(selected),
		itemIDs(pool),
		membershipCredential(options.IdentityToken, token),
	); err != nil {
		return Session{}, err
	}

	return Session{
		Code:  code,
		Token: token,
	}, nil
}

// roomLifetime returns a requested room lifetime or the configured default.
func (s *Service) roomLifetime(hours int) time.Duration {
	if hours == 0 {
		return s.roomTTL
	}
	return time.Duration(hours) * time.Hour
}

// normalizeCreateRoomOptions validates and canonicalizes room creation input.
func normalizeCreateRoomOptions(options CreateOptions) (CreateOptions, error) {
	options.Name = cleanName(options.Name)

	if options.Name == "" {
		return CreateOptions{}, InvalidInput("name is required")
	}

	if len(options.LibraryKeys) == 0 {
		return CreateOptions{}, InvalidInput("select at least one library")
	}

	if err := validateFilters(options.Filters); err != nil {
		return CreateOptions{}, err
	}

	if !ValidRoundSize(options.RoundSize) {
		return CreateOptions{}, InvalidInputf(
			"round size must be between 0 and %d titles",
			maxRoundSize,
		)
	}

	if !ValidRoomLifetimeHours(options.LifetimeHours) {
		return CreateOptions{}, InvalidInputf(
			"room lifetime must be between %d and %d hours",
			minimumRoomLifetimeHours,
			maximumRoomLifetimeHours,
		)
	}

	options.GenreMode = normalizeGenreMode(options.GenreMode)

	if options.GenreMode == "" {
		return CreateOptions{}, InvalidInput(
			"genre mode must be any or all",
		)
	}

	options.Sampling = cmp.Or(options.Sampling, SamplingRandom)

	if !ValidSamplingStrategy(options.Sampling) {
		return CreateOptions{}, InvalidInput(
			"invalid first-round selection strategy",
		)
	}

	return options, nil
}

// ValidRoundSize reports whether the requested first-round limit is supported.
func ValidRoundSize(roundSize int) bool {
	return roundSize >= 0 && roundSize <= maxRoundSize
}

// ValidRoomLifetimeHours reports whether a room lifetime uses the default or supported bounds.
func ValidRoomLifetimeHours(hours int) bool {
	return hours == 0 || hours >= minimumRoomLifetimeHours && hours <= maximumRoomLifetimeHours
}

// ValidGenreMode reports whether a personal genre mode is supported or omitted.
func ValidGenreMode(mode GenreMode) bool {
	return mode == "" || mode == GenreModeAny || mode == GenreModeAll
}
