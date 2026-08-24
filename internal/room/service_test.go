package room

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/gi8lino/screendeck/internal/media"
	"github.com/gi8lino/screendeck/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCatalog is an in-memory catalog used by room service tests.
type fakeCatalog struct {
	// libraries contains fake media libraries.
	libraries []media.Library
	// items contains fake media items.
	items map[string][]media.Item
}

// Libraries returns the fake catalog libraries.
func (f fakeCatalog) Libraries(context.Context) ([]media.Library, error) {
	return f.libraries, nil
}

// Items returns fake media for the selected library.
func (f fakeCatalog) Items(
	_ context.Context,
	library media.Library,
) ([]media.Item, error) {
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
	assert.Equal(t, "drama", hostState.Candidate.ID)

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
	assert.Equal(t, "action", guestState.Candidate.ID)
	assert.Equal(t, []string{"Action"}, guestState.Me.Genres)
}

func TestCreateRoundSizeLimitsInitialDeck(t *testing.T) {
	t.Parallel()

	database, err := store.Open(":memory:")
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

func TestSelectInitialItems(t *testing.T) {
	t.Parallel()

	items := []media.Item{
		{
			ID:      "old",
			Title:   "Old",
			Rating:  7.0,
			AddedAt: 100,
			Viewed:  false,
		},
		{
			ID:      "top",
			Title:   "Top",
			Rating:  9.5,
			AddedAt: 200,
			Viewed:  true,
		},
		{
			ID:      "new",
			Title:   "New",
			Rating:  8.0,
			AddedAt: 300,
			Viewed:  false,
		},
	}

	t.Run("random", func(t *testing.T) {
		t.Parallel()

		selected, err := selectInitialItems(items, SamplingRandom, 0)
		require.NoError(t, err)
		assert.ElementsMatch(t, itemIDs(items), itemIDs(selected))
	})

	t.Run("highest rated with limit", func(t *testing.T) {
		t.Parallel()

		selected, err := selectInitialItems(items, SamplingHighestRated, 2)
		require.NoError(t, err)
		require.Len(t, selected, 2)
		assert.Equal(t, "top", selected[0].ID)
		assert.Equal(t, "new", selected[1].ID)
	})

	t.Run("recently added with limit", func(t *testing.T) {
		t.Parallel()

		selected, err := selectInitialItems(items, SamplingRecentlyAdded, 2)
		require.NoError(t, err)
		require.Len(t, selected, 2)
		assert.Equal(t, "new", selected[0].ID)
		assert.Equal(t, "top", selected[1].ID)
	})

	t.Run("random unwatched", func(t *testing.T) {
		t.Parallel()

		selected, err := selectInitialItems(items, SamplingRandomUnwatched, 0)
		require.NoError(t, err)
		assert.Len(t, selected, 2)
		assert.False(t, selected[0].Viewed)
		assert.False(t, selected[1].Viewed)
	})

	t.Run("invalid strategy", func(t *testing.T) {
		t.Parallel()

		_, err := selectInitialItems(items, SamplingStrategy("bogus"), 0)
		require.Error(t, err)
	})
}

func TestParticipantGenreModeAllRequiresEveryGenre(t *testing.T) {
	t.Parallel()

	database, err := store.Open(":memory:")
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
	assert.Equal(t, "combo", state.Candidate.ID)
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

func TestCollectCatalogGenres(t *testing.T) {
	t.Parallel()

	t.Run("collects non-empty genres", func(t *testing.T) {
		t.Parallel()

		genres := make(map[string]struct{})
		collectCatalogGenres(genres, []string{"Action", "Drama", ""})

		assert.Equal(t, map[string]struct{}{
			"Action": {},
			"Drama":  {},
		}, genres)
	})

	t.Run("ignores whitespace-only genres", func(t *testing.T) {
		t.Parallel()

		genres := make(map[string]struct{})
		collectCatalogGenres(genres, []string{" ", "\t", "\n"})

		assert.Empty(t, genres)
	})
}

func TestUpdateCatalogYearBounds(t *testing.T) {
	t.Parallel()

	t.Run("sets initial bounds", func(t *testing.T) {
		t.Parallel()

		options := CatalogOptions{}
		updateCatalogYearBounds(&options, 2020)

		assert.Equal(t, 2020, options.MinYear)
		assert.Equal(t, 2020, options.MaxYear)
	})

	t.Run("expands lower bound", func(t *testing.T) {
		t.Parallel()

		options := CatalogOptions{MinYear: 2020, MaxYear: 2024}
		updateCatalogYearBounds(&options, 2010)

		assert.Equal(t, 2010, options.MinYear)
		assert.Equal(t, 2024, options.MaxYear)
	})

	t.Run("expands upper bound", func(t *testing.T) {
		t.Parallel()

		options := CatalogOptions{MinYear: 2010, MaxYear: 2020}
		updateCatalogYearBounds(&options, 2026)

		assert.Equal(t, 2010, options.MinYear)
		assert.Equal(t, 2026, options.MaxYear)
	})

	t.Run("ignores unknown year", func(t *testing.T) {
		t.Parallel()

		options := CatalogOptions{MinYear: 2010, MaxYear: 2026}
		updateCatalogYearBounds(&options, 0)

		assert.Equal(t, 2010, options.MinYear)
		assert.Equal(t, 2026, options.MaxYear)
	})
}

func TestValidRoundSize(t *testing.T) {
	t.Parallel()

	t.Run("zero is valid", func(t *testing.T) {
		t.Parallel()
		assert.True(t, validRoundSize(0))
	})

	t.Run("positive value is valid", func(t *testing.T) {
		t.Parallel()
		assert.True(t, validRoundSize(100))
	})

	t.Run("maximum is valid", func(t *testing.T) {
		t.Parallel()
		assert.True(t, validRoundSize(maxRoundSize))
	})

	t.Run("negative value is invalid", func(t *testing.T) {
		t.Parallel()
		assert.False(t, validRoundSize(-1))
	})

	t.Run("above maximum is invalid", func(t *testing.T) {
		t.Parallel()
		assert.False(t, validRoundSize(maxRoundSize+1))
	})
}

func TestHasNegativeFilterValue(t *testing.T) {
	t.Parallel()

	t.Run("non-negative filters", func(t *testing.T) {
		t.Parallel()
		assert.False(t, hasNegativeFilterValue(Filters{
			YearFrom:           1990,
			YearTo:             2026,
			MaxDurationMinutes: 180,
		}))
	})

	t.Run("negative year from", func(t *testing.T) {
		t.Parallel()
		assert.True(t, hasNegativeFilterValue(Filters{YearFrom: -1}))
	})

	t.Run("negative year to", func(t *testing.T) {
		t.Parallel()
		assert.True(t, hasNegativeFilterValue(Filters{YearTo: -1}))
	})

	t.Run("negative duration", func(t *testing.T) {
		t.Parallel()
		assert.True(t, hasNegativeFilterValue(Filters{MaxDurationMinutes: -1}))
	})
}

func TestHasReversedYearRange(t *testing.T) {
	t.Parallel()

	t.Run("ascending range", func(t *testing.T) {
		t.Parallel()
		assert.False(t, hasReversedYearRange(Filters{YearFrom: 2000, YearTo: 2020}))
	})

	t.Run("equal bounds", func(t *testing.T) {
		t.Parallel()
		assert.False(t, hasReversedYearRange(Filters{YearFrom: 2020, YearTo: 2020}))
	})

	t.Run("open range", func(t *testing.T) {
		t.Parallel()
		assert.False(t, hasReversedYearRange(Filters{YearFrom: 2020}))
	})

	t.Run("reversed range", func(t *testing.T) {
		t.Parallel()
		assert.True(t, hasReversedYearRange(Filters{YearFrom: 2025, YearTo: 2020}))
	})
}

func TestSortItemsByRating(t *testing.T) {
	t.Parallel()

	items := []media.Item{
		{ID: "bravo", Title: "Bravo", Rating: 8.0},
		{ID: "top", Title: "Top", Rating: 9.5},
		{ID: "alpha", Title: "Alpha", Rating: 8.0},
	}

	sortItemsByRating(items)

	assert.Equal(t, []string{"top", "alpha", "bravo"}, itemIDs(items))
}

func TestSortItemsByAddedAt(t *testing.T) {
	t.Parallel()

	items := []media.Item{
		{ID: "bravo", Title: "Bravo", AddedAt: 200},
		{ID: "new", Title: "New", AddedAt: 300},
		{ID: "alpha", Title: "Alpha", AddedAt: 200},
	}

	sortItemsByAddedAt(items)

	assert.Equal(t, []string{"new", "alpha", "bravo"}, itemIDs(items))
}

func TestUnwatchedItems(t *testing.T) {
	t.Parallel()

	items := []media.Item{
		{ID: "first", Viewed: false},
		{ID: "watched", Viewed: true},
		{ID: "second", Viewed: false},
	}

	unwatched := unwatchedItems(items)

	assert.Equal(t, []string{"first", "second"}, itemIDs(unwatched))
}

func TestCacheEntryFresh(t *testing.T) {
	t.Parallel()

	t.Run("fresh entry", func(t *testing.T) {
		t.Parallel()
		assert.True(t, cacheEntryFresh(cacheEntry{
			fetchedAt: time.Now().Add(-libraryCacheTTL / 2),
		}, true))
	})

	t.Run("expired entry", func(t *testing.T) {
		t.Parallel()
		assert.False(t, cacheEntryFresh(cacheEntry{
			fetchedAt: time.Now().Add(-libraryCacheTTL - time.Second),
		}, true))
	})

	t.Run("missing entry", func(t *testing.T) {
		t.Parallel()
		assert.False(t, cacheEntryFresh(cacheEntry{}, false))
	})
}

func TestMatchesWatchFilter(t *testing.T) {
	t.Parallel()

	t.Run("allows watched item without unwatched filter", func(t *testing.T) {
		t.Parallel()
		assert.True(t, matchesWatchFilter(
			media.Item{Viewed: true},
			Filters{},
		))
	})

	t.Run("rejects watched item when unwatched only", func(t *testing.T) {
		t.Parallel()
		assert.False(t, matchesWatchFilter(
			media.Item{Viewed: true},
			Filters{UnwatchedOnly: true},
		))
	})

	t.Run("allows unwatched item when unwatched only", func(t *testing.T) {
		t.Parallel()
		assert.True(t, matchesWatchFilter(
			media.Item{Viewed: false},
			Filters{UnwatchedOnly: true},
		))
	})
}

func TestMatchesYearFilter(t *testing.T) {
	t.Parallel()

	t.Run("inside range", func(t *testing.T) {
		t.Parallel()
		assert.True(t, matchesYearFilter(
			media.Item{Year: 2020},
			Filters{YearFrom: 2010, YearTo: 2025},
		))
	})

	t.Run("below lower bound", func(t *testing.T) {
		t.Parallel()
		assert.False(t, matchesYearFilter(
			media.Item{Year: 2009},
			Filters{YearFrom: 2010},
		))
	})

	t.Run("above upper bound", func(t *testing.T) {
		t.Parallel()
		assert.False(t, matchesYearFilter(
			media.Item{Year: 2026},
			Filters{YearTo: 2025},
		))
	})

	t.Run("without bounds", func(t *testing.T) {
		t.Parallel()
		assert.True(t, matchesYearFilter(
			media.Item{Year: 1980},
			Filters{},
		))
	})
}

func TestMatchesDurationFilter(t *testing.T) {
	t.Parallel()

	t.Run("movie inside limit", func(t *testing.T) {
		t.Parallel()
		assert.True(t, matchesDurationFilter(
			media.Item{Type: "movie", Duration: 90 * 60 * 1000},
			Filters{MaxDurationMinutes: 120},
		))
	})

	t.Run("movie above limit", func(t *testing.T) {
		t.Parallel()
		assert.False(t, matchesDurationFilter(
			media.Item{Type: "movie", Duration: 121 * 60 * 1000},
			Filters{MaxDurationMinutes: 120},
		))
	})

	t.Run("show ignores movie duration limit", func(t *testing.T) {
		t.Parallel()
		assert.True(t, matchesDurationFilter(
			media.Item{Type: "show", Duration: 500 * 60 * 1000},
			Filters{MaxDurationMinutes: 120},
		))
	})

	t.Run("disabled limit", func(t *testing.T) {
		t.Parallel()
		assert.True(t, matchesDurationFilter(
			media.Item{Type: "movie", Duration: 500 * 60 * 1000},
			Filters{},
		))
	})
}

func TestMatchesGenreFilter(t *testing.T) {
	t.Parallel()

	t.Run("without genre filter", func(t *testing.T) {
		t.Parallel()
		assert.True(t, matchesGenreFilter(
			media.Item{Genres: []string{"Drama"}},
			nil,
		))
	})

	t.Run("matches case-insensitively", func(t *testing.T) {
		t.Parallel()
		assert.True(t, matchesGenreFilter(
			media.Item{Genres: []string{" Action "}},
			map[string]struct{}{"action": {}},
		))
	})

	t.Run("rejects unmatched genre", func(t *testing.T) {
		t.Parallel()
		assert.False(t, matchesGenreFilter(
			media.Item{Genres: []string{"Drama"}},
			map[string]struct{}{"action": {}},
		))
	})
}

func TestNormalizeGenreKey(t *testing.T) {
	t.Parallel()

	t.Run("trims and lowercases", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "science fiction", normalizeGenreKey("  Science Fiction  "))
	})

	t.Run("empty input", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, normalizeGenreKey("   "))
	})
}
