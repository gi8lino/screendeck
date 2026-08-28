package room

import (
	"cmp"
	"context"
	"slices"
	"strings"
)

// Genres returns the genres currently represented in a room's deck.
func (s *Service) Genres(
	ctx context.Context,
	code string,
) ([]string, error) {
	code = strings.ToUpper(strings.TrimSpace(code))

	if len(code) != 6 {
		return nil, InvalidInput(
			"a six-character room code is required",
		)
	}

	return s.store.RoomGenres(ctx, code)
}

// normalizeGenreMode returns a supported personal genre matching mode.
func normalizeGenreMode(mode GenreMode) GenreMode {
	mode = cmp.Or(mode, GenreModeAny)
	if ValidGenreMode(mode) {
		return mode
	}
	return ""
}

// canonicalGenres validates participant genres and returns their canonical room spelling.
func canonicalGenres(
	selected,
	available []string,
) ([]string, error) {
	canonical := make(map[string]string, len(available))
	collectGenres(canonical, available)

	seen := make(map[string]struct{}, len(selected))
	result := make([]string, 0, len(selected))

	for _, genre := range selected {
		trimmed := strings.TrimSpace(genre)
		if trimmed == "" {
			continue
		}

		key := normalizeGenreKey(trimmed)

		value, ok := canonical[key]
		if !ok {
			return nil, InvalidInputf(
				"genre %q is not available in this room",
				trimmed,
			)
		}

		if _, duplicate := seen[key]; duplicate {
			continue
		}

		seen[key] = struct{}{}
		result = append(result, value)
	}

	slices.Sort(result)

	return result, nil
}

// normalizeGenreKey returns the case-insensitive comparison key for a genre.
func normalizeGenreKey(genre string) string {
	return strings.ToLower(strings.TrimSpace(genre))
}

// collectGenres adds non-empty genres to a case-insensitive canonical set.
func collectGenres(
	target map[string]string,
	genres []string,
) {
	for _, genre := range genres {
		trimmed := strings.TrimSpace(genre)
		if trimmed == "" {
			continue
		}

		target[normalizeGenreKey(trimmed)] = trimmed
	}
}
