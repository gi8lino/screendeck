package room

import "context"

// TestCreateRoomOptions exposes room creation inputs to external integration tests.
type TestCreateRoomOptions = CreateOptions

// CreateForTest exercises room creation without requiring an HTTP handler.
func (s *Service) CreateForTest(ctx context.Context, options TestCreateRoomOptions) (Session, error) {
	return s.create(ctx, options)
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
