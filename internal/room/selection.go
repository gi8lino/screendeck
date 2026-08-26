package room

import (
	"errors"
	mathrand "math/rand/v2"
	"sort"

	"github.com/gi8lino/screendeck/internal/media"
)

// filterEligibleItems applies room filters and removes duplicate media item identifiers.
func filterEligibleItems(
	items []media.Item,
	filters Filters,
) []media.Item {
	genres := normalizedGenreSet(filters.Genres)
	seen := make(map[string]struct{}, len(items))
	eligible := make([]media.Item, 0, len(items))

	for _, item := range items {
		if !matchesFilters(item, filters, genres) {
			continue
		}

		if _, duplicate := seen[item.ID]; duplicate {
			continue
		}

		seen[item.ID] = struct{}{}
		eligible = append(eligible, item)
	}

	return eligible
}

// normalizedGenreSet returns non-empty room filter genres in lower-case form.
func normalizedGenreSet(genres []string) map[string]struct{} {
	result := make(map[string]struct{}, len(genres))

	for _, genre := range genres {
		normalized := normalizeGenreKey(genre)
		if normalized == "" {
			continue
		}

		result[normalized] = struct{}{}
	}

	return result
}

// genresFromItems returns the canonical set of genres represented by the supplied items.
func genresFromItems(items []media.Item) []string {
	genres := make(map[string]string)

	for _, item := range items {
		collectGenres(genres, item.Genres)
	}

	return genreValues(genres)
}

// limitItems returns the first limit items while preserving the original ordering.
func limitItems(
	items []media.Item,
	limit int,
) []media.Item {
	if limit > 0 && len(items) > limit {
		return items[:limit]
	}

	return items
}

// validateFilters verifies room-wide catalog filter bounds.
func validateFilters(filters Filters) error {
	if hasNegativeFilterValue(filters) || ReversedYearRange(filters) {
		return errors.New("invalid catalog filters")
	}

	return nil
}

// hasNegativeFilterValue reports whether a numeric room filter is below zero.
func hasNegativeFilterValue(filters Filters) bool {
	return filters.YearFrom < 0 ||
		filters.YearTo < 0 ||
		filters.MaxDurationMinutes < 0
}

// ReversedYearRange reports whether both year bounds are set in descending order.
func ReversedYearRange(filters Filters) bool {
	return filters.YearFrom > 0 &&
		filters.YearTo > 0 &&
		filters.YearFrom > filters.YearTo
}

// ValidSamplingStrategy reports whether a first-round selection strategy is supported.
func ValidSamplingStrategy(strategy SamplingStrategy) bool {
	switch strategy {
	case SamplingRandom,
		SamplingHighestRated,
		SamplingRecentlyAdded,
		SamplingRandomUnwatched:
		return true

	default:
		return false
	}
}

// orderInitialItems orders and filters the room's original eligible pool.
func orderInitialItems(
	items []media.Item,
	strategy SamplingStrategy,
) ([]media.Item, error) {
	selected := append([]media.Item(nil), items...)

	switch strategy {
	case SamplingRandom:
		shuffleItems(selected)

	case SamplingHighestRated:
		sortItemsByRating(selected)

	case SamplingRecentlyAdded:
		sortItemsByAddedAt(selected)

	case SamplingRandomUnwatched:
		selected = unwatchedItems(selected)
		if len(selected) == 0 {
			return nil, errors.New(
				"no unwatched titles match the selected room filters",
			)
		}

		shuffleItems(selected)

	default:
		return nil, errors.New(
			"invalid first-round selection strategy",
		)
	}

	return selected, nil
}

// shuffleItems randomizes media items in place.
func shuffleItems(items []media.Item) {
	mathrand.Shuffle(len(items), func(i, j int) {
		items[i], items[j] = items[j], items[i]
	})
}

// sortItemsByRating orders media by descending rating and then title.
func sortItemsByRating(items []media.Item) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Rating != items[j].Rating {
			return items[i].Rating > items[j].Rating
		}

		return items[i].Title < items[j].Title
	})
}

// sortItemsByAddedAt orders media by newest provider added-at time and then title.
func sortItemsByAddedAt(items []media.Item) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].AddedAt != items[j].AddedAt {
			return items[i].AddedAt > items[j].AddedAt
		}

		return items[i].Title < items[j].Title
	})
}

// unwatchedItems returns only media that the provider has not marked as viewed.
func unwatchedItems(items []media.Item) []media.Item {
	selected := make([]media.Item, 0, len(items))

	for _, item := range items {
		if item.Viewed {
			continue
		}

		selected = append(selected, item)
	}

	return selected
}

// selectInitialItems returns the first limited portion of the ordered eligible pool.
func selectInitialItems(
	items []media.Item,
	strategy SamplingStrategy,
	limit int,
) ([]media.Item, error) {
	selected, err := orderInitialItems(items, strategy)
	if err != nil {
		return nil, err
	}

	return limitItems(selected, limit), nil
}

// itemIDs converts media items to their media item identifiers in order.
func itemIDs(items []media.Item) []string {
	keys := make([]string, 0, len(items))

	for _, item := range items {
		keys = append(keys, item.ID)
	}

	return keys
}
