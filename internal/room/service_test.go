package room

import (
	"testing"
	"time"

	"github.com/gi8lino/screendeck/internal/media"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
		assert.True(t, ValidRoundSize(0))
	})

	t.Run("positive value is valid", func(t *testing.T) {
		t.Parallel()
		assert.True(t, ValidRoundSize(100))
	})

	t.Run("maximum is valid", func(t *testing.T) {
		t.Parallel()
		assert.True(t, ValidRoundSize(maxRoundSize))
	})

	t.Run("negative value is invalid", func(t *testing.T) {
		t.Parallel()
		assert.False(t, ValidRoundSize(-1))
	})

	t.Run("above maximum is invalid", func(t *testing.T) {
		t.Parallel()
		assert.False(t, ValidRoundSize(maxRoundSize+1))
	})
}

// TestValidRoomLifetimeHours verifies default and bounded room lifetimes.
func TestValidRoomLifetimeHours(t *testing.T) {
	t.Parallel()

	assert.True(t, ValidRoomLifetimeHours(0))
	assert.True(t, ValidRoomLifetimeHours(6))
	assert.True(t, ValidRoomLifetimeHours(24))
	assert.True(t, ValidRoomLifetimeHours(7*24))
	assert.False(t, ValidRoomLifetimeHours(5))
	assert.False(t, ValidRoomLifetimeHours(7*24+1))
}

func TestValidGenreMode(t *testing.T) {
	t.Parallel()

	assert.True(t, ValidGenreMode(""))
	assert.True(t, ValidGenreMode(GenreModeAny))
	assert.True(t, ValidGenreMode(GenreModeAll))
	assert.False(t, ValidGenreMode(GenreMode("invalid")))
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

func TestReversedYearRange(t *testing.T) {
	t.Parallel()

	t.Run("ascending range", func(t *testing.T) {
		t.Parallel()
		assert.False(t, ReversedYearRange(Filters{YearFrom: 2000, YearTo: 2020}))
	})

	t.Run("equal bounds", func(t *testing.T) {
		t.Parallel()
		assert.False(t, ReversedYearRange(Filters{YearFrom: 2020, YearTo: 2020}))
	})

	t.Run("open range", func(t *testing.T) {
		t.Parallel()
		assert.False(t, ReversedYearRange(Filters{YearFrom: 2020}))
	})

	t.Run("reversed range", func(t *testing.T) {
		t.Parallel()
		assert.True(t, ReversedYearRange(Filters{YearFrom: 2025, YearTo: 2020}))
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
