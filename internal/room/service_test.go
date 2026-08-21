package room

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/gi8lino/screendeck/internal/plex"
	"github.com/gi8lino/screendeck/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCatalog is an in-memory catalog used by room service tests.
type fakeCatalog struct {
	// libraries contains fake Plex libraries.
	libraries []plex.Library
	// items contains cached Plex items.
	items map[string][]plex.Item
}

// Libraries returns the fake catalog libraries.
func (f fakeCatalog) Libraries(context.Context) ([]plex.Library, error) {
	return f.libraries, nil
}

// Items returns fake media for the selected library.
func (f fakeCatalog) Items(
	_ context.Context,
	library plex.Library,
) ([]plex.Item, error) {
	return f.items[library.Key], nil
}

// Poster returns no poster from the fake catalog.
func (f fakeCatalog) Poster(
	context.Context,
	string,
) (*http.Response, error) {
	return nil, nil
}

func TestLibraries(t *testing.T) {
	t.Parallel()

	database, err := store.Open(":memory:")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	catalog := fakeCatalog{
		libraries: []plex.Library{
			{Key: "1", Title: "Films", Type: "movie"},
			{Key: "2", Title: "Kids", Type: "movie"},
			{Key: "3", Title: "Archive", Type: "movie"},
			{Key: "4", Title: "Series", Type: "show"},
		},
		items: map[string][]plex.Item{
			"1": {
				{
					RatingKey: "film",
					Library:   "1",
					Type:      "movie",
					Title:     "Film",
				},
			},
			"2": {
				{
					RatingKey: "kids",
					Library:   "2",
					Type:      "movie",
					Title:     "Kids Film",
				},
			},
			"3": {
				{
					RatingKey: "archive",
					Library:   "3",
					Type:      "movie",
					Title:     "Archived Film",
				},
			},
			"4": {
				{
					RatingKey: "series",
					Library:   "4",
					Type:      "show",
					Title:     "Series",
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

	assert.Equal(t, []plex.Library{
		{Key: "1", Title: "Films", Type: "movie"},
		{Key: "4", Title: "Series", Type: "show"},
	}, libraries)

	_, err = service.Options(
		context.Background(),
		[]string{"2"},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `media library "2" not found`)

	_, err = service.create(
		context.Background(),
		createRoomOptions{
			name:        "Host",
			libraryKeys: []string{"3"},
			filters:     Filters{},
			genres:      nil,
			genreMode:   GenreModeAny,
			sampling:    SamplingRandom,
			roundSize:   0,
		},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `media library "3" not found`)
}

func TestCatalogOptionsAndFilters(t *testing.T) {
	t.Parallel()

	database, err := store.Open(":memory:")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	catalog := fakeCatalog{
		libraries: []plex.Library{
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
		items: map[string][]plex.Item{
			"1": {
				{
					RatingKey: "m1",
					Library:   "1",
					Type:      "movie",
					Title:     "Old Action",
					Year:      2010,
					Duration:  90 * 60 * 1000,
					Genres:    []string{"Action"},
				},
				{
					RatingKey: "m2",
					Library:   "1",
					Type:      "movie",
					Title:     "Long Drama",
					Year:      2023,
					Duration:  200 * 60 * 1000,
					Genres:    []string{"Drama"},
				},
			},
			"2": {
				{
					RatingKey: "s1",
					Library:   "2",
					Type:      "show",
					Title:     "TV Drama",
					Year:      2022,
					Genres:    []string{"Drama"},
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

	session, err := service.create(
		context.Background(),
		createRoomOptions{
			name:        "Host",
			libraryKeys: []string{"1", "2"},
			filters: Filters{
				Genres:             []string{"Drama"},
				YearFrom:           2020,
				MaxDurationMinutes: 120,
			},
			genres:    nil,
			genreMode: GenreModeAny,
			sampling:  SamplingRandom,
			roundSize: 0,
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

	database, err := store.Open(":memory:")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	catalog := fakeCatalog{
		libraries: []plex.Library{
			{
				Key:   "1",
				Title: "Films",
				Type:  "movie",
			},
		},
		items: map[string][]plex.Item{
			"1": {
				{
					RatingKey: "action",
					Library:   "1",
					Type:      "movie",
					Title:     "Action Pick",
					Genres:    []string{"Action"},
				},
				{
					RatingKey: "drama",
					Library:   "1",
					Type:      "movie",
					Title:     "Drama Pick",
					Genres:    []string{"Drama"},
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

	host, err := service.create(
		context.Background(),
		createRoomOptions{
			name:        "Host",
			libraryKeys: []string{"1"},
			filters:     Filters{},
			genres:      []string{"Drama"},
			genreMode:   GenreModeAny,
			sampling:    SamplingRandom,
			roundSize:   0,
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
	assert.Equal(t, "drama", hostState.Candidate.RatingKey)

	guest, err := service.join(
		context.Background(),
		host.Code,
		"Guest",
		[]string{"Action"},
		GenreModeAny,
		"",
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
	assert.Equal(t, "action", guestState.Candidate.RatingKey)
	assert.Equal(t, []string{"Action"}, guestState.Me.Genres)
}

func TestCreateRoundSizeLimitsInitialDeck(t *testing.T) {
	t.Parallel()

	database, err := store.Open(":memory:")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	catalog := fakeCatalog{
		libraries: []plex.Library{
			{
				Key:   "1",
				Title: "Films",
				Type:  "movie",
			},
		},
		items: map[string][]plex.Item{
			"1": {
				{
					RatingKey: "a",
					Library:   "1",
					Type:      "movie",
					Title:     "Alpha",
				},
				{
					RatingKey: "b",
					Library:   "1",
					Type:      "movie",
					Title:     "Beta",
				},
				{
					RatingKey: "c",
					Library:   "1",
					Type:      "movie",
					Title:     "Gamma",
				},
				{
					RatingKey: "d",
					Library:   "1",
					Type:      "movie",
					Title:     "Delta",
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

	session, err := service.create(
		context.Background(),
		createRoomOptions{
			name:        "Host",
			libraryKeys: []string{"1"},
			filters:     Filters{},
			genres:      nil,
			genreMode:   GenreModeAny,
			sampling:    SamplingRandom,
			roundSize:   2,
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

	_, err = service.create(
		context.Background(),
		createRoomOptions{
			name:        "Host",
			libraryKeys: []string{"1"},
			filters:     Filters{},
			genres:      nil,
			genreMode:   GenreModeAny,
			sampling:    SamplingRandom,
			roundSize:   -1,
		},
	)
	require.Error(t, err)
}

func TestInitialSamplingStrategies(t *testing.T) {
	t.Parallel()

	items := []plex.Item{
		{
			RatingKey: "old",
			Title:     "Old",
			Rating:    7.0,
			AddedAt:   100,
			Viewed:    false,
		},
		{
			RatingKey: "top",
			Title:     "Top",
			Rating:    9.5,
			AddedAt:   200,
			Viewed:    true,
		},
		{
			RatingKey: "new",
			Title:     "New",
			Rating:    8.0,
			AddedAt:   300,
			Viewed:    false,
		},
	}

	highest, err := selectInitialItems(
		items,
		SamplingHighestRated,
		2,
	)
	require.NoError(t, err)
	require.Len(t, highest, 2)
	assert.Equal(t, "top", highest[0].RatingKey)
	assert.Equal(t, "new", highest[1].RatingKey)

	recent, err := selectInitialItems(
		items,
		SamplingRecentlyAdded,
		2,
	)
	require.NoError(t, err)
	require.Len(t, recent, 2)
	assert.Equal(t, "new", recent[0].RatingKey)
	assert.Equal(t, "top", recent[1].RatingKey)

	unwatched, err := selectInitialItems(
		items,
		SamplingRandomUnwatched,
		0,
	)
	require.NoError(t, err)
	assert.Len(t, unwatched, 2)
	assert.False(t, unwatched[0].Viewed)
	assert.False(t, unwatched[1].Viewed)

	_, err = selectInitialItems(
		items,
		SamplingStrategy("bogus"),
		0,
	)
	require.Error(t, err)
}

func TestParticipantGenreModeAllRequiresEveryGenre(t *testing.T) {
	t.Parallel()

	database, err := store.Open(":memory:")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	catalog := fakeCatalog{
		libraries: []plex.Library{
			{
				Key:   "1",
				Title: "Films",
				Type:  "movie",
			},
		},
		items: map[string][]plex.Item{
			"1": {
				{
					RatingKey: "action",
					Library:   "1",
					Type:      "movie",
					Title:     "Action",
					Genres:    []string{"Action"},
				},
				{
					RatingKey: "combo",
					Library:   "1",
					Type:      "movie",
					Title:     "Action Drama",
					Genres:    []string{"Action", "Drama"},
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

	host, err := service.create(
		context.Background(),
		createRoomOptions{
			name:        "Host",
			libraryKeys: []string{"1"},
			filters:     Filters{},
			genres:      []string{"Action", "Drama"},
			genreMode:   GenreModeAll,
			sampling:    SamplingHighestRated,
			roundSize:   0,
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
	assert.Equal(t, "combo", state.Candidate.RatingKey)
	assert.Equal(t, "all", state.Me.GenreMode)
}

func TestValidateFilters(t *testing.T) {
	t.Parallel()

	t.Run("valid", func(t *testing.T) {
		t.Parallel()
		require.NoError(t,
			validateFilters(
				Filters{
					YearFrom:           1990,
					YearTo:             2026,
					MaxDurationMinutes: 180,
				},
			),
		)
	})

	t.Run("negative year from", func(t *testing.T) {
		t.Parallel()
		require.Error(t, validateFilters(Filters{YearFrom: -1}))
	})

	t.Run("negative year to", func(t *testing.T) {
		t.Parallel()
		require.Error(t, validateFilters(Filters{YearTo: -1}))
	})

	t.Run("negative duration", func(t *testing.T) {
		t.Parallel()
		require.Error(t, validateFilters(Filters{MaxDurationMinutes: -1}))
	})

	t.Run("reversed years", func(t *testing.T) {
		t.Parallel()
		require.Error(t, validateFilters(Filters{YearFrom: 2025, YearTo: 2020}))
	})
}
