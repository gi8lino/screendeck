package room

import "context"

// TestCreateRoomOptions exposes room creation inputs to external integration tests.
type TestCreateRoomOptions struct {
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

// CreateForTest exercises room creation without requiring an HTTP handler.
func (s *Service) CreateForTest(ctx context.Context, options TestCreateRoomOptions) (Session, error) {
	return s.create(ctx, createRoomOptions{
		name:          options.Name,
		libraryKeys:   options.LibraryKeys,
		filters:       options.Filters,
		genres:        options.Genres,
		genreMode:     options.GenreMode,
		sampling:      options.Sampling,
		roundSize:     options.RoundSize,
		lifetimeHours: options.LifetimeHours,
		identityToken: options.IdentityToken,
	})
}

// JoinForTest exercises room joining without requiring an HTTP handler.
func (s *Service) JoinForTest(
	ctx context.Context,
	code string,
	name string,
	genres []string,
	genreMode GenreMode,
	identityToken string,
) (Session, error) {
	return s.join(ctx, code, name, genres, genreMode, identityToken)
}
