package room

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/gi8lino/screendeck/internal/media"
)

// Libraries returns the media libraries that are available for room creation.
func (s *Service) Libraries(ctx context.Context) ([]media.Library, error) {
	if s.catalog == nil {
		return nil, media.ErrNotConfigured
	}

	libraries, err := s.catalog.Libraries(ctx)
	if err != nil {
		return nil, err
	}

	visible := make([]media.Library, 0, len(libraries))

	for _, library := range libraries {
		if s.libraryExcluded(library) {
			continue
		}

		visible = append(visible, library)
	}

	return visible, nil
}

// normalizeLibraryExclusions returns trimmed case-insensitive library exclusion values.
func normalizeLibraryExclusions(values []string) map[string]struct{} {
	excluded := make(map[string]struct{}, len(values))

	for _, value := range values {
		normalized := strings.ToLower(strings.TrimSpace(value))
		if normalized == "" {
			continue
		}

		excluded[normalized] = struct{}{}
	}

	return excluded
}

// libraryExcluded reports whether a media library is excluded by title or key.
func (s *Service) libraryExcluded(library media.Library) bool {
	if len(s.excludedLibraries) == 0 {
		return false
	}

	key := strings.ToLower(strings.TrimSpace(library.Key))
	title := strings.ToLower(strings.TrimSpace(library.Title))

	_, keyExcluded := s.excludedLibraries[key]
	_, titleExcluded := s.excludedLibraries[title]

	return keyExcluded || titleExcluded
}

// Options returns filter options found in the selected libraries.
func (s *Service) Options(
	ctx context.Context,
	libraryKeys []string,
) (CatalogOptions, error) {
	_, items, err := s.loadItems(ctx, libraryKeys)
	if err != nil {
		return CatalogOptions{}, err
	}

	return catalogOptions(items), nil
}

// catalogOptions derives available genres and year bounds from media items.
func catalogOptions(items []media.Item) CatalogOptions {
	genreSet := make(map[string]struct{})
	options := CatalogOptions{}

	for _, item := range items {
		collectCatalogGenres(genreSet, item.Genres)
		updateCatalogYearBounds(&options, item.Year)
	}

	options.Genres = make([]string, 0, len(genreSet))
	for genre := range genreSet {
		options.Genres = append(options.Genres, genre)
	}
	slices.Sort(options.Genres)

	return options
}

// collectCatalogGenres adds non-empty catalog genres while preserving their provider spelling.
func collectCatalogGenres(
	target map[string]struct{},
	genres []string,
) {
	for _, genre := range genres {
		if strings.TrimSpace(genre) == "" {
			continue
		}

		target[genre] = struct{}{}
	}
}

// updateCatalogYearBounds expands the catalog year range with a media item's year.
func updateCatalogYearBounds(
	options *CatalogOptions,
	year int,
) {
	if year <= 0 {
		return
	}

	if options.MinYear == 0 || year < options.MinYear {
		options.MinYear = year
	}

	if year > options.MaxYear {
		options.MaxYear = year
	}
}

// loadItems resolves selected libraries and loads their media items.
func (s *Service) loadItems(
	ctx context.Context,
	libraryKeys []string,
) (selected []media.Library, items []media.Item, err error) {
	if len(libraryKeys) == 0 {
		return nil, nil, InvalidInput("select at least one library")
	}

	libraries, err := s.Libraries(ctx)
	if err != nil {
		return nil, nil, err
	}

	available := librariesByKey(libraries)

	selected = make([]media.Library, 0, len(libraryKeys))

	for _, key := range libraryKeys {
		library, ok := available[key]
		if !ok {
			return nil, nil, InvalidInputf(
				"media library %q not found",
				key,
			)
		}

		libraryItems, err := s.libraryItems(ctx, library)
		if err != nil {
			return nil, nil, fmt.Errorf(
				"load %s: %w",
				library.Title,
				err,
			)
		}

		selected = append(selected, library)
		items = append(items, libraryItems...)
	}

	return selected, items, nil
}

// librariesByKey indexes media libraries by their stable provider key.
func librariesByKey(
	libraries []media.Library,
) map[string]media.Library {
	indexed := make(map[string]media.Library, len(libraries))

	for _, library := range libraries {
		indexed[library.Key] = library
	}

	return indexed
}

// libraryItems returns cached or freshly loaded items for a library.
func (s *Service) libraryItems(
	ctx context.Context,
	library media.Library,
) ([]media.Item, error) {
	s.mu.Lock()
	cached, ok := s.cache[library.Key]
	s.mu.Unlock()

	if cacheEntryFresh(cached, ok) {
		return cached.items, nil
	}

	items, err := s.catalog.Items(ctx, library)
	if err != nil {
		return nil, err
	}

	if err := s.catalogStore.SaveLibrary(ctx, library, items); err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.cache[library.Key] = cacheEntry{
		items:     items,
		fetchedAt: time.Now(),
	}
	s.mu.Unlock()

	return items, nil
}

// cacheEntryFresh reports whether a cached library result can still be reused.
func cacheEntryFresh(
	entry cacheEntry,
	ok bool,
) bool {
	return ok && time.Since(entry.fetchedAt) < libraryCacheTTL
}

// SortLibraries orders libraries by media type and title.
func SortLibraries(libraries []media.Library) {
	slices.SortFunc(libraries, func(a, b media.Library) int {
		return cmp.Or(
			cmp.Compare(a.Type, b.Type),
			cmp.Compare(a.Title, b.Title),
		)
	})
}
