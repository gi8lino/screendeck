package room_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/gi8lino/screendeck/internal/media"
	. "github.com/gi8lino/screendeck/internal/room"
	"github.com/gi8lino/screendeck/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCatalog is an in-memory catalog used by room service integration tests.
type fakeCatalog struct {
	libraries []media.Library
	items     map[string][]media.Item
}

// Libraries returns the fake catalog libraries.
func (f fakeCatalog) Libraries(context.Context) ([]media.Library, error) {
	return f.libraries, nil
}

// Items returns fake media for the selected library.
func (f fakeCatalog) Items(_ context.Context, library media.Library) ([]media.Item, error) {
	return f.items[library.Key], nil
}

// Poster returns no poster from the fake catalog.
func (f fakeCatalog) Poster(context.Context, string) (*http.Response, error) {
	return nil, nil
}

func TestLibraries(t *testing.T) {
	t.Parallel()

	database, err := store.Open(":memory:", "")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	catalog := fakeCatalog{
		libraries: []media.Library{
			{Key: "1", Title: "Films", Type: "movie"},
			{Key: "2", Title: "Kids", Type: "movie"},
			{Key: "3", Title: "Archive", Type: "movie"},
			{Key: "4", Title: "Series", Type: "show"},
		},
		items: map[string][]media.Item{
			"1": {
				{
					ID:         "film",
					LibraryKey: "1",
					Type:       "movie",
					Title:      "Film",
				},
			},
			"2": {
				{
					ID:         "kids",
					LibraryKey: "2",
					Type:       "movie",
					Title:      "Kids Film",
				},
			},
			"3": {
				{
					ID:         "archive",
					LibraryKey: "3",
					Type:       "movie",
					Title:      "Archived Film",
				},
			},
			"4": {
				{
					ID:         "series",
					LibraryKey: "4",
					Type:       "show",
					Title:      "Series",
				},
			},
		},
	}

	service := NewService(
		database,
		catalog,
		time.Hour,
		[]string{" kids ", "3"},
	)

	libraries, err := service.Libraries(context.Background())
	require.NoError(t, err)

	assert.Equal(t, []media.Library{
		{Key: "1", Title: "Films", Type: "movie"},
		{Key: "4", Title: "Series", Type: "show"},
	}, libraries)

	_, err = service.Options(
		context.Background(),
		[]string{"2"},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `media library "2" not found`)

	_, err = service.CreateForTest(
		context.Background(),
		TestCreateRoomOptions{
			Name:        "Host",
			LibraryKeys: []string{"3"},
			Filters:     Filters{},
			Genres:      nil,
			GenreMode:   GenreModeAny,
			Sampling:    SamplingRandom,
			RoundSize:   0,
		},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `media library "3" not found`)
}

func TestCatalogOptionsAndFilters(t *testing.T) {
	t.Parallel()

	database, err := store.Open(":memory:", "")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	catalog := fakeCatalog{
		libraries: []media.Library{
			{
				Key:   "1",
				Title: "Films",
				Type:  "movie",
			},
			{
				Key:   "2",
				Title: "Series",
				Type:  "show",
			},
		},
		items: map[string][]media.Item{
			"1": {
				{
					ID:         "m1",
					LibraryKey: "1",
					Type:       "movie",
					Title:      "Old Action",
					Year:       2010,
					Duration:   90 * 60 * 1000,
					Genres:     []string{"Action"},
				},
				{
					ID:         "m2",
					LibraryKey: "1",
					Type:       "movie",
					Title:      "Long Drama",
					Year:       2023,
					Duration:   200 * 60 * 1000,
					Genres:     []string{"Drama"},
				},
			},
			"2": {
				{
					ID:         "s1",
					LibraryKey: "2",
					Type:       "show",
					Title:      "TV Drama",
					Year:       2022,
					Genres:     []string{"Drama"},
				},
			},
		},
	}

	service := NewService(
		database,
		catalog,
		time.Hour,
		nil,
	)

	options, err := service.Options(
		context.Background(),
		[]string{"1", "2"},
	)
	require.NoError(t, err)

	assert.Len(t, options.Genres, 2)
	assert.Equal(t, 2010, options.MinYear)
	assert.Equal(t, 2023, options.MaxYear)

	session, err := service.CreateForTest(
		context.Background(),
		TestCreateRoomOptions{
			Name:        "Host",
			LibraryKeys: []string{"1", "2"},
			Filters: Filters{
				Genres:             []string{"Drama"},
				YearFrom:           2020,
				MaxDurationMinutes: 120,
			},
			Genres:    nil,
			GenreMode: GenreModeAny,
			Sampling:  SamplingRandom,
			RoundSize: 0,
		},
	)
	require.NoError(t, err)

	state, err := service.State(
		context.Background(),
		session.Code,
		session.Token,
	)
	require.NoError(t, err)

	assert.Equal(t, 1, state.Progress.Total)
	require.NotNil(t, state.Candidate)
	assert.Equal(t, "show", state.Candidate.Type)
}

func TestParticipantGenresFilterPersonalDecks(t *testing.T) {
	t.Parallel()

	database, err := store.Open(":memory:", "")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	catalog := fakeCatalog{
		libraries: []media.Library{
			{
				Key:   "1",
				Title: "Films",
				Type:  "movie",
			},
		},
		items: map[string][]media.Item{
			"1": {
				{
					ID:         "action",
					LibraryKey: "1",
					Type:       "movie",
					Title:      "Action Pick",
					Genres:     []string{"Action"},
				},
				{
					ID:         "drama",
					LibraryKey: "1",
					Type:       "movie",
					Title:      "Drama Pick",
					Genres:     []string{"Drama"},
				},
			},
		},
	}

	service := NewService(
		database,
		catalog,
		time.Hour,
		nil,
	)

	host, err := service.CreateForTest(
		context.Background(),
		TestCreateRoomOptions{
			Name:          "Host",
			LibraryKeys:   []string{"1"},
			Filters:       Filters{},
			Genres:        []string{"Drama"},
			GenreMode:     GenreModeAny,
			Sampling:      SamplingRandom,
			RoundSize:     0,
			IdentityToken: "aG9zdC1pZGVudGl0eQ",
		},
	)
	require.NoError(t, err)

	hostState, err := service.State(
		context.Background(),
		host.Code,
		host.Token,
	)
	require.NoError(t, err)

	assert.Equal(t, 1, hostState.Progress.Total)
	assert.Equal(t, 2, hostState.Progress.RoundTotal)
	assert.Equal(t, 1, hostState.Progress.FilteredOut)
	require.NotNil(t, hostState.Candidate)
	assert.Equal(t, "drama", hostState.Candidate.ID)

	guest, err := service.JoinForTest(
		context.Background(),
		host.Code,
		"Guest",
		[]string{"Action"},
		GenreModeAny,
		"Z3Vlc3QtaWRlbnRpdHk",
	)
	require.NoError(t, err)

	guestState, err := service.State(
		context.Background(),
		guest.Code,
		guest.Token,
	)
	require.NoError(t, err)

	assert.Equal(t, 1, guestState.Progress.Total)
	assert.Equal(t, 2, guestState.Progress.RoundTotal)
	assert.Equal(t, 1, guestState.Progress.FilteredOut)
	require.NotNil(t, guestState.Candidate)
	assert.Equal(t, "action", guestState.Candidate.ID)
	assert.Equal(t, []string{"Action"}, guestState.Me.Genres)
}

func TestCreateRoundSizeLimitsInitialDeck(t *testing.T) {
	t.Parallel()

	database, err := store.Open(":memory:", "")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	catalog := fakeCatalog{
		libraries: []media.Library{
			{
				Key:   "1",
				Title: "Films",
				Type:  "movie",
			},
		},
		items: map[string][]media.Item{
			"1": {
				{
					ID:         "a",
					LibraryKey: "1",
					Type:       "movie",
					Title:      "Alpha",
				},
				{
					ID:         "b",
					LibraryKey: "1",
					Type:       "movie",
					Title:      "Beta",
				},
				{
					ID:         "c",
					LibraryKey: "1",
					Type:       "movie",
					Title:      "Gamma",
				},
				{
					ID:         "d",
					LibraryKey: "1",
					Type:       "movie",
					Title:      "Delta",
				},
			},
		},
	}

	service := NewService(
		database,
		catalog,
		time.Hour,
		nil,
	)

	session, err := service.CreateForTest(
		context.Background(),
		TestCreateRoomOptions{
			Name:        "Host",
			LibraryKeys: []string{"1"},
			Filters:     Filters{},
			Genres:      nil,
			GenreMode:   GenreModeAny,
			Sampling:    SamplingRandom,
			RoundSize:   2,
		},
	)
	require.NoError(t, err)

	state, err := service.State(
		context.Background(),
		session.Code,
		session.Token,
	)
	require.NoError(t, err)

	assert.Equal(t, 2, state.Progress.Total)
	require.NotNil(t, state.Candidate)

	_, err = service.CreateForTest(
		context.Background(),
		TestCreateRoomOptions{
			Name:        "Host",
			LibraryKeys: []string{"1"},
			Filters:     Filters{},
			Genres:      nil,
			GenreMode:   GenreModeAny,
			Sampling:    SamplingRandom,
			RoundSize:   -1,
		},
	)
	require.Error(t, err)
}

func TestParticipantGenreModeAllRequiresEveryGenre(t *testing.T) {
	t.Parallel()

	database, err := store.Open(":memory:", "")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	catalog := fakeCatalog{
		libraries: []media.Library{
			{
				Key:   "1",
				Title: "Films",
				Type:  "movie",
			},
		},
		items: map[string][]media.Item{
			"1": {
				{
					ID:         "action",
					LibraryKey: "1",
					Type:       "movie",
					Title:      "Action",
					Genres:     []string{"Action"},
				},
				{
					ID:         "combo",
					LibraryKey: "1",
					Type:       "movie",
					Title:      "Action Drama",
					Genres:     []string{"Action", "Drama"},
				},
			},
		},
	}

	service := NewService(
		database,
		catalog,
		time.Hour,
		nil,
	)

	host, err := service.CreateForTest(
		context.Background(),
		TestCreateRoomOptions{
			Name:        "Host",
			LibraryKeys: []string{"1"},
			Filters:     Filters{},
			Genres:      []string{"Action", "Drama"},
			GenreMode:   GenreModeAll,
			Sampling:    SamplingHighestRated,
			RoundSize:   0,
		},
	)
	require.NoError(t, err)

	state, err := service.State(
		context.Background(),
		host.Code,
		host.Token,
	)
	require.NoError(t, err)

	assert.Equal(t, 1, state.Progress.Total)
	require.NotNil(t, state.Candidate)
	assert.Equal(t, "combo", state.Candidate.ID)
	assert.Equal(t, "all", state.Me.GenreMode)
}
