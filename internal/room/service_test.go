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
func (f fakeCatalog) Libraries(context.Context) ([]plex.Library, error) { return f.libraries, nil }

// Items returns fake media for the selected library.
func (f fakeCatalog) Items(_ context.Context, library plex.Library) ([]plex.Item, error) {
	return f.items[library.Key], nil
}

// Poster returns no poster from the fake catalog.
func (f fakeCatalog) Poster(context.Context, string) (*http.Response, error) { return nil, nil }

// TestLibraries verifies configured Plex library exclusions are enforced.
func TestLibraries(t *testing.T) {
	catalog := fakeCatalog{
		libraries: []plex.Library{
			{Key: "1", Title: "Films", Type: "movie"},
			{Key: "2", Title: "Kids", Type: "movie"},
			{Key: "3", Title: "Archive", Type: "movie"},
			{Key: "4", Title: "Series", Type: "show"},
		},
		items: map[string][]plex.Item{
			"1": {{RatingKey: "film", Library: "1", Type: "movie", Title: "Film"}},
			"2": {{RatingKey: "kids", Library: "2", Type: "movie", Title: "Kids Film"}},
			"3": {{RatingKey: "archive", Library: "3", Type: "movie", Title: "Archived Film"}},
			"4": {{RatingKey: "series", Library: "4", Type: "show", Title: "Series"}},
		},
	}

	t.Run("filters configured libraries", func(t *testing.T) {
		database, err := store.Open(":memory:")
		require.NoError(t, err)
		defer database.Close() // nolint:errcheck
		service := NewService(database, catalog, time.Hour, []string{" kids ", "3"})

		libraries, err := service.Libraries(t.Context())
		require.NoError(t, err)
		assert.Equal(t, []plex.Library{
			{Key: "1", Title: "Films", Type: "movie"},
			{Key: "4", Title: "Series", Type: "show"},
		}, libraries)
	})

	t.Run("excluded library cannot be used for options", func(t *testing.T) {
		database, err := store.Open(":memory:")
		require.NoError(t, err)
		defer database.Close() // nolint:errcheck
		service := NewService(database, catalog, time.Hour, []string{" kids ", "3"})

		_, err = service.Options(t.Context(), []string{"2"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), `media library "2" not found`)
	})

	t.Run("excluded library cannot create room", func(t *testing.T) {
		database, err := store.Open(":memory:")
		require.NoError(t, err)
		defer database.Close() // nolint:errcheck
		service := NewService(database, catalog, time.Hour, []string{" kids ", "3"})

		_, err = service.CreateForIdentity(
			t.Context(),
			"Host",
			[]string{"3"},
			Filters{},
			nil,
			GenreModeAny,
			SamplingRandom,
			0,
			"host-browser",
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `media library "3" not found`)
	})
}

// TestCatalogOptionsAndFilters verifies catalog option discovery and room filtering.
func TestCatalogOptionsAndFilters(t *testing.T) {
	catalog := fakeCatalog{
		libraries: []plex.Library{{Key: "1", Title: "Films", Type: "movie"}, {Key: "2", Title: "Series", Type: "show"}},
		items: map[string][]plex.Item{
			"1": {
				{RatingKey: "m1", Library: "1", Type: "movie", Title: "Old Action", Year: 2010, Duration: 90 * 60 * 1000, Genres: []string{"Action"}},
				{RatingKey: "m2", Library: "1", Type: "movie", Title: "Long Drama", Year: 2023, Duration: 200 * 60 * 1000, Genres: []string{"Drama"}},
			},
			"2": {{RatingKey: "s1", Library: "2", Type: "show", Title: "TV Drama", Year: 2022, Genres: []string{"Drama"}}},
		},
	}

	t.Run("discovers options", func(t *testing.T) {
		database, err := store.Open(":memory:")
		require.NoError(t, err)
		defer database.Close() // nolint:errcheck
		service := NewService(database, catalog, time.Hour, nil)

		options, err := service.Options(t.Context(), []string{"1", "2"})
		require.NoError(t, err)
		assert.Len(t, options.Genres, 2)
		assert.Equal(t, 2010, options.MinYear)
		assert.Equal(t, 2023, options.MaxYear)
	})

	t.Run("applies room filters", func(t *testing.T) {
		database, err := store.Open(":memory:")
		require.NoError(t, err)
		defer database.Close() // nolint:errcheck
		service := NewService(database, catalog, time.Hour, nil)

		session, err := service.CreateForIdentity(
			t.Context(),
			"Host",
			[]string{"1", "2"},
			Filters{Genres: []string{"Drama"}, YearFrom: 2020, MaxDurationMinutes: 120},
			nil,
			GenreModeAny,
			SamplingRandom,
			0,
			"host-browser",
		)
		require.NoError(t, err)

		state, err := service.State(t.Context(), session.Code, session.Token)
		require.NoError(t, err)
		assert.Equal(t, 1, state.Progress.Total)
		require.NotNil(t, state.Candidate)
		assert.Equal(t, "show", state.Candidate.Type)
	})
}

// TestParticipantGenresFilterPersonalDecks verifies each participant can narrow only their own candidates.
func TestParticipantGenresFilterPersonalDecks(t *testing.T) {
	database, err := store.Open(":memory:")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	catalog := fakeCatalog{
		libraries: []plex.Library{{Key: "1", Title: "Films", Type: "movie"}},
		items: map[string][]plex.Item{
			"1": {
				{RatingKey: "action", Library: "1", Type: "movie", Title: "Action Pick", Genres: []string{"Action"}},
				{RatingKey: "drama", Library: "1", Type: "movie", Title: "Drama Pick", Genres: []string{"Drama"}},
			},
		},
	}
	service := NewService(database, catalog, time.Hour, nil)

	host, err := service.CreateForIdentity(t.Context(), "Host", []string{"1"}, Filters{}, []string{"Drama"}, GenreModeAny, SamplingRandom, 0, "host-browser")
	require.NoError(t, err)

	hostState, err := service.State(t.Context(), host.Code, host.Token)
	require.NoError(t, err)
	assert.Equal(t, 1, hostState.Progress.Total)
	assert.Equal(t, 2, hostState.Progress.RoundTotal)
	assert.Equal(t, 1, hostState.Progress.FilteredOut)
	require.NotNil(t, hostState.Candidate)
	assert.Equal(t, "drama", hostState.Candidate.RatingKey)

	guest, err := service.JoinForIdentity(t.Context(), host.Code, "Guest", []string{"Action"}, GenreModeAny, "guest-browser")
	require.NoError(t, err)

	guestState, err := service.State(t.Context(), guest.Code, guest.Token)
	require.NoError(t, err)
	assert.Equal(t, 1, guestState.Progress.Total)
	assert.Equal(t, 2, guestState.Progress.RoundTotal)
	assert.Equal(t, 1, guestState.Progress.FilteredOut)
	require.NotNil(t, guestState.Candidate)
	assert.Equal(t, "action", guestState.Candidate.RatingKey)
	assert.Equal(t, []string{"Action"}, guestState.Me.Genres)
}

// TestCreateRoundSizeLimitsInitialDeck verifies first-round size limits and validation.
func TestCreateRoundSizeLimitsInitialDeck(t *testing.T) {
	catalog := fakeCatalog{
		libraries: []plex.Library{{Key: "1", Title: "Films", Type: "movie"}},
		items: map[string][]plex.Item{
			"1": {
				{RatingKey: "a", Library: "1", Type: "movie", Title: "Alpha"},
				{RatingKey: "b", Library: "1", Type: "movie", Title: "Beta"},
				{RatingKey: "c", Library: "1", Type: "movie", Title: "Gamma"},
				{RatingKey: "d", Library: "1", Type: "movie", Title: "Delta"},
			},
		},
	}

	t.Run("limits initial deck", func(t *testing.T) {
		database, err := store.Open(":memory:")
		require.NoError(t, err)
		defer database.Close() // nolint:errcheck
		service := NewService(database, catalog, time.Hour, nil)

		session, err := service.CreateForIdentity(t.Context(), "Host", []string{"1"}, Filters{}, nil, GenreModeAny, SamplingRandom, 2, "host-browser")
		require.NoError(t, err)

		state, err := service.State(t.Context(), session.Code, session.Token)
		require.NoError(t, err)
		assert.Equal(t, 2, state.Progress.Total)
		require.NotNil(t, state.Candidate)
	})

	t.Run("rejects negative size", func(t *testing.T) {
		database, err := store.Open(":memory:")
		require.NoError(t, err)
		defer database.Close() // nolint:errcheck
		service := NewService(database, catalog, time.Hour, nil)

		_, err = service.CreateForIdentity(t.Context(), "Host", []string{"1"}, Filters{}, nil, GenreModeAny, SamplingRandom, -1, "host-browser")
		require.Error(t, err)
	})
}

// TestSelectInitialItems verifies first-round sampling strategies and validation.
func TestSelectInitialItems(t *testing.T) {
	items := []plex.Item{
		{RatingKey: "old", Title: "Old", Rating: 7.0, AddedAt: 100, Viewed: false},
		{RatingKey: "top", Title: "Top", Rating: 9.5, AddedAt: 200, Viewed: true},
		{RatingKey: "new", Title: "New", Rating: 8.0, AddedAt: 300, Viewed: false},
	}

	t.Run("highest rated", func(t *testing.T) {
		selected, err := selectInitialItems(items, SamplingHighestRated, 2)
		require.NoError(t, err)
		require.Len(t, selected, 2)
		assert.Equal(t, "top", selected[0].RatingKey)
		assert.Equal(t, "new", selected[1].RatingKey)
	})

	t.Run("recently added", func(t *testing.T) {
		selected, err := selectInitialItems(items, SamplingRecentlyAdded, 2)
		require.NoError(t, err)
		require.Len(t, selected, 2)
		assert.Equal(t, "new", selected[0].RatingKey)
		assert.Equal(t, "top", selected[1].RatingKey)
	})

	t.Run("random unwatched", func(t *testing.T) {
		selected, err := selectInitialItems(items, SamplingRandomUnwatched, 0)
		require.NoError(t, err)
		assert.Len(t, selected, 2)
		assert.False(t, selected[0].Viewed)
		assert.False(t, selected[1].Viewed)
	})

	t.Run("invalid strategy", func(t *testing.T) {
		_, err := selectInitialItems(items, SamplingStrategy("bogus"), 0)
		require.Error(t, err)
	})
}

// TestParticipantGenreModeAllRequiresEveryGenre verifies all-mode narrows a personal deck to titles containing every selected genre.
func TestParticipantGenreModeAllRequiresEveryGenre(t *testing.T) {
	database, err := store.Open(":memory:")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	catalog := fakeCatalog{
		libraries: []plex.Library{{Key: "1", Title: "Films", Type: "movie"}},
		items: map[string][]plex.Item{
			"1": {
				{RatingKey: "action", Library: "1", Type: "movie", Title: "Action", Genres: []string{"Action"}},
				{RatingKey: "combo", Library: "1", Type: "movie", Title: "Action Drama", Genres: []string{"Action", "Drama"}},
			},
		},
	}
	service := NewService(database, catalog, time.Hour, nil)

	host, err := service.CreateForIdentity(t.Context(), "Host", []string{"1"}, Filters{}, []string{"Action", "Drama"}, GenreModeAll, SamplingHighestRated, 0, "host-browser")
	require.NoError(t, err)
	state, err := service.State(t.Context(), host.Code, host.Token)
	require.NoError(t, err)
	assert.Equal(t, 1, state.Progress.Total)
	require.NotNil(t, state.Candidate)
	assert.Equal(t, "combo", state.Candidate.RatingKey)
	assert.Equal(t, "all", state.Me.GenreMode)
}

// TestValidateFilters verifies room filter bounds reject invalid ranges and negative values.
func TestValidateFilters(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		require.NoError(t, validateFilters(Filters{YearFrom: 1990, YearTo: 2026, MaxDurationMinutes: 180}))
	})

	t.Run("negative year from", func(t *testing.T) {
		require.Error(t, validateFilters(Filters{YearFrom: -1}))
	})

	t.Run("negative year to", func(t *testing.T) {
		require.Error(t, validateFilters(Filters{YearTo: -1}))
	})

	t.Run("negative duration", func(t *testing.T) {
		require.Error(t, validateFilters(Filters{MaxDurationMinutes: -1}))
	})

	t.Run("reversed years", func(t *testing.T) {
		require.Error(t, validateFilters(Filters{YearFrom: 2025, YearTo: 2020}))
	})
}

// TestCatalogOptions verifies option discovery returns sorted unique genres and valid year bounds.
func TestCatalogOptions(t *testing.T) {
	options := catalogOptions([]plex.Item{
		{Year: 2019, Genres: []string{"Drama", "Science Fiction"}},
		{Year: 2024, Genres: []string{"Drama", "", "Mystery"}},
		{Year: 0, Genres: []string{"Comedy"}},
	})

	assert.Equal(t, []string{"Comedy", "Drama", "Mystery", "Science Fiction"}, options.Genres)
	assert.Equal(t, 2019, options.MinYear)
	assert.Equal(t, 2024, options.MaxYear)
}

// TestNormalizeCreateRoomOptions verifies room creation defaults and validation.
func TestNormalizeCreateRoomOptions(t *testing.T) {
	t.Run("normalizes defaults", func(t *testing.T) {
		normalized, err := normalizeCreateRoomOptions(createRoomOptions{
			name:        "  Alice   Example  ",
			libraryKeys: []string{"1"},
			filters:     Filters{YearFrom: 2000, YearTo: 2026},
		})
		require.NoError(t, err)
		assert.Equal(t, "Alice Example", normalized.name)
		assert.Equal(t, GenreModeAny, normalized.genreMode)
		assert.Equal(t, SamplingRandom, normalized.sampling)
	})

	t.Run("requires name", func(t *testing.T) {
		_, err := normalizeCreateRoomOptions(createRoomOptions{libraryKeys: []string{"1"}})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name is required")
	})

	t.Run("requires library", func(t *testing.T) {
		_, err := normalizeCreateRoomOptions(createRoomOptions{name: "Host"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "select at least one library")
	})

	t.Run("rejects oversized round", func(t *testing.T) {
		_, err := normalizeCreateRoomOptions(createRoomOptions{
			name:        "Host",
			libraryKeys: []string{"1"},
			roundSize:   50001,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "round size")
	})

	t.Run("rejects invalid genre mode", func(t *testing.T) {
		_, err := normalizeCreateRoomOptions(createRoomOptions{
			name:        "Host",
			libraryKeys: []string{"1"},
			genreMode:   GenreMode("invalid"),
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "genre mode")
	})

	t.Run("rejects invalid sampling strategy", func(t *testing.T) {
		_, err := normalizeCreateRoomOptions(createRoomOptions{
			name:        "Host",
			libraryKeys: []string{"1"},
			sampling:    SamplingStrategy("invalid"),
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "selection strategy")
	})
}

// TestFilterEligibleItems verifies room filters are applied and duplicate Plex items are removed.
func TestFilterEligibleItems(t *testing.T) {
	items := []plex.Item{
		{RatingKey: "keep-movie", Type: "movie", Year: 2022, Duration: 100 * 60 * 1000, Genres: []string{"Drama"}},
		{RatingKey: "keep-movie", Type: "movie", Year: 2022, Duration: 100 * 60 * 1000, Genres: []string{"Drama"}},
		{RatingKey: "watched", Type: "movie", Year: 2023, Duration: 90 * 60 * 1000, Genres: []string{"Drama"}, Viewed: true},
		{RatingKey: "old", Type: "movie", Year: 2010, Duration: 90 * 60 * 1000, Genres: []string{"Drama"}},
		{RatingKey: "long", Type: "movie", Year: 2024, Duration: 180 * 60 * 1000, Genres: []string{"Drama"}},
		{RatingKey: "wrong-genre", Type: "movie", Year: 2024, Duration: 90 * 60 * 1000, Genres: []string{"Comedy"}},
		{RatingKey: "keep-show", Type: "show", Year: 2024, Duration: 10 * 60 * 60 * 1000, Genres: []string{"Drama"}},
	}
	filters := Filters{
		Genres:             []string{" drama "},
		YearFrom:           2020,
		MaxDurationMinutes: 120,
		UnwatchedOnly:      true,
	}

	eligible := filterEligibleItems(items, filters)
	require.Len(t, eligible, 2)
	assert.Equal(t, "keep-movie", eligible[0].RatingKey)
	assert.Equal(t, "keep-show", eligible[1].RatingKey)
}

// TestNormalizedGenreSet verifies genre filters are trimmed, lower-cased, and deduplicated.
func TestNormalizedGenreSet(t *testing.T) {
	genres := normalizedGenreSet([]string{" Drama ", "SCIENCE FICTION", "drama", "", "   "})

	assert.Equal(t, map[string]struct{}{
		"drama":           {},
		"science fiction": {},
	}, genres)
}

// TestGenresFromItems verifies media genres are deduplicated case-insensitively and sorted.
func TestGenresFromItems(t *testing.T) {
	genres := genresFromItems([]plex.Item{
		{Genres: []string{"Drama", "Science Fiction"}},
		{Genres: []string{"Comedy", "Drama", ""}},
	})

	assert.Equal(t, []string{"Comedy", "Drama", "Science Fiction"}, genres)
}

// TestLimitItems verifies optional deck limits preserve input ordering.
func TestLimitItems(t *testing.T) {
	items := []plex.Item{{RatingKey: "a"}, {RatingKey: "b"}, {RatingKey: "c"}}

	t.Run("limits items", func(t *testing.T) {
		limited := limitItems(items, 2)
		require.Len(t, limited, 2)
		assert.Equal(t, "a", limited[0].RatingKey)
		assert.Equal(t, "b", limited[1].RatingKey)
	})

	t.Run("zero keeps all items", func(t *testing.T) {
		assert.Equal(t, items, limitItems(items, 0))
	})

	t.Run("large limit keeps all items", func(t *testing.T) {
		assert.Equal(t, items, limitItems(items, 10))
	})
}

// TestLibrariesByKey verifies Plex libraries are indexed by their stable section key.
func TestLibrariesByKey(t *testing.T) {
	libraries := []plex.Library{
		{Key: "1", Title: "Films", Type: "movie"},
		{Key: "2", Title: "Series", Type: "show"},
	}

	indexed := librariesByKey(libraries)
	require.Len(t, indexed, 2)
	assert.Equal(t, libraries[0], indexed["1"])
	assert.Equal(t, libraries[1], indexed["2"])
}

// TestNormalizeJoinInput verifies room join input is canonicalized and validated.
func TestNormalizeJoinInput(t *testing.T) {
	t.Run("normalizes input", func(t *testing.T) {
		code, name, mode, err := normalizeJoinInput(" ab12cd ", "  Alice   Example  ", "")
		require.NoError(t, err)
		assert.Equal(t, "AB12CD", code)
		assert.Equal(t, "Alice Example", name)
		assert.Equal(t, GenreModeAny, mode)
	})

	t.Run("rejects invalid code", func(t *testing.T) {
		_, _, _, err := normalizeJoinInput("short", "Alice", GenreModeAny)
		require.Error(t, err)
	})

	t.Run("rejects empty name", func(t *testing.T) {
		_, _, _, err := normalizeJoinInput("AB12CD", "   ", GenreModeAny)
		require.Error(t, err)
	})

	t.Run("rejects invalid genre mode", func(t *testing.T) {
		_, _, _, err := normalizeJoinInput("AB12CD", "Alice", GenreMode("invalid"))
		require.Error(t, err)
	})
}
