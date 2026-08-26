package room

import "github.com/gi8lino/screendeck/internal/media"

// matchesFilters reports whether a media item satisfies room filters.
func matchesFilters(
	item media.Item,
	filters Filters,
	genres map[string]struct{},
) bool {
	if !matchesWatchFilter(item, filters) {
		return false
	}

	if !matchesYearFilter(item, filters) {
		return false
	}

	if !matchesDurationFilter(item, filters) {
		return false
	}

	return matchesGenreFilter(item, genres)
}

// matchesWatchFilter reports whether an item satisfies the watched-state filter.
func matchesWatchFilter(
	item media.Item,
	filters Filters,
) bool {
	return !filters.UnwatchedOnly || !item.Viewed
}

// matchesYearFilter reports whether an item falls inside the configured year range.
func matchesYearFilter(
	item media.Item,
	filters Filters,
) bool {
	if filters.YearFrom > 0 && item.Year < filters.YearFrom {
		return false
	}

	if filters.YearTo > 0 && item.Year > filters.YearTo {
		return false
	}

	return true
}

// matchesDurationFilter reports whether an item satisfies the movie duration limit.
func matchesDurationFilter(
	item media.Item,
	filters Filters,
) bool {
	if filters.MaxDurationMinutes <= 0 || item.Type != "movie" {
		return true
	}

	maxDuration := filters.MaxDurationMinutes * 60 * 1000

	return item.Duration <= maxDuration
}

// matchesGenreFilter reports whether an item contains one of the selected room genres.
func matchesGenreFilter(
	item media.Item,
	genres map[string]struct{},
) bool {
	if len(genres) == 0 {
		return true
	}

	for _, genre := range item.Genres {
		if _, ok := genres[normalizeGenreKey(genre)]; ok {
			return true
		}
	}

	return false
}
