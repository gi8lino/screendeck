package room

import (
	"cmp"
	"context"
	"strings"
	"time"
)

// createRoomOptions contains normalized input for creating a room.
type createRoomOptions struct {
	// name is the host display name.
	name string
	// libraryKeys identifies the media libraries included in the room.
	libraryKeys []string
	// filters contains room-wide catalog filters.
	filters Filters
	// genres contains the host's personal genre preferences.
	genres []string
	// genreMode controls how host genres are matched.
	genreMode GenreMode
	// sampling controls first-round ordering.
	sampling SamplingStrategy
	// roundSize limits the first-round deck when non-zero.
	roundSize int
	// lifetimeHours overrides the configured room lifetime when non-zero.
	lifetimeHours int
	// identityToken identifies the host's persistent browser membership.
	identityToken string
}

// CreateForIdentity creates a room and associates the host with a persistent browser identity.
func (s *Service) CreateForIdentity(
	ctx context.Context,
	name string,
	libraryKeys []string,
	filters Filters,
	genres []string,
	genreMode GenreMode,
	sampling SamplingStrategy,
	roundSize int,
	lifetimeHours int,
	identityToken string,
) (Session, error) {
	if strings.TrimSpace(identityToken) == "" {
		return Session{}, InvalidInput("browser identity is required")
	}

	return s.create(ctx, createRoomOptions{
		name:          name,
		libraryKeys:   libraryKeys,
		filters:       filters,
		genres:        genres,
		genreMode:     genreMode,
		sampling:      sampling,
		roundSize:     roundSize,
		lifetimeHours: lifetimeHours,
		identityToken: identityToken,
	})
}

// create creates a room with a persistent browser identity association.
func (s *Service) create(
	ctx context.Context,
	options createRoomOptions,
) (Session, error) {
	// Normalize and validate room input before touching the catalog or store.
	options, err := normalizeCreateRoomOptions(options)
	if err != nil {
		return Session{}, err
	}

	// Build the complete eligible pool, then apply the first-round limit.
	_, items, err := s.loadItems(ctx, options.libraryKeys)
	if err != nil {
		return Session{}, err
	}

	eligible := filterEligibleItems(items, options.filters)
	if len(eligible) == 0 {
		return Session{}, InvalidInput("the selected libraries contain no matching titles")
	}

	pool, err := orderInitialItems(eligible, options.sampling)
	if err != nil {
		return Session{}, err
	}

	participantGenres, err := canonicalGenres(
		options.genres,
		genresFromItems(pool),
	)
	if err != nil {
		return Session{}, err
	}

	selected := limitItems(pool, options.roundSize)

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

	if err := s.store.CreateRoom(
		ctx,
		Room{
			Code:      code,
			Round:     1,
			CreatedAt: now,
			ExpiresAt: now.Add(s.roomLifetime(options.lifetimeHours)),
		},
		Participant{
			ID:        participantID,
			Name:      options.name,
			Genres:    participantGenres,
			GenreMode: string(options.genreMode),
		},
		tokenHash,
		itemIDs(selected),
		itemIDs(pool),
		membershipCredential(options.identityToken, token),
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
func normalizeCreateRoomOptions(options createRoomOptions) (createRoomOptions, error) {
	options.name = cleanName(options.name)

	if options.name == "" {
		return createRoomOptions{}, InvalidInput("name is required")
	}

	if len(options.libraryKeys) == 0 {
		return createRoomOptions{}, InvalidInput("select at least one library")
	}

	if err := validateFilters(options.filters); err != nil {
		return createRoomOptions{}, err
	}

	if !ValidRoundSize(options.roundSize) {
		return createRoomOptions{}, InvalidInputf(
			"round size must be between 0 and %d titles",
			maxRoundSize,
		)
	}

	if !ValidRoomLifetimeHours(options.lifetimeHours) {
		return createRoomOptions{}, InvalidInputf(
			"room lifetime must be between %d and %d hours",
			minimumRoomLifetimeHours,
			maximumRoomLifetimeHours,
		)
	}

	options.genreMode = normalizeGenreMode(options.genreMode)

	if options.genreMode == "" {
		return createRoomOptions{}, InvalidInput(
			"genre mode must be any or all",
		)
	}

	options.sampling = cmp.Or(options.sampling, SamplingRandom)

	if !ValidSamplingStrategy(options.sampling) {
		return createRoomOptions{}, InvalidInput(
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
