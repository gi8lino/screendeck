package room

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/gi8lino/screendeck/internal/plex"
	"github.com/gi8lino/screendeck/internal/store"
)

type fakeCatalog struct {
	libraries []plex.Library
	items     map[string][]plex.Item
}

// Libraries returns the fake catalog libraries.
func (f fakeCatalog) Libraries(context.Context) ([]plex.Library, error) { return f.libraries, nil }

// Items returns fake media for the selected library.
func (f fakeCatalog) Items(_ context.Context, library plex.Library) ([]plex.Item, error) {
	return f.items[library.Key], nil
}

// Poster returns no poster from the fake catalog.
func (f fakeCatalog) Poster(context.Context, string) (*http.Response, error) { return nil, nil }

// TestCatalogOptionsAndFilters verifies catalog option discovery and item filtering.
func TestCatalogOptionsAndFilters(t *testing.T) {
	database, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
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
	service := NewService(database, catalog, time.Hour)
	options, err := service.Options(context.Background(), []string{"1", "2"})
	if err != nil || len(options.Genres) != 2 || options.MinYear != 2010 || options.MaxYear != 2023 {
		t.Fatalf("options=%#v err=%v", options, err)
	}
	session, err := service.Create(context.Background(), "Host", []string{"1", "2"}, Filters{Genres: []string{"Drama"}, YearFrom: 2020, MaxDurationMinutes: 120})
	if err != nil {
		t.Fatal(err)
	}
	state, err := service.State(context.Background(), session.Code, session.Token)
	if err != nil || state.Progress.Total != 1 || state.Candidate == nil || state.Candidate.Type != "show" {
		t.Fatalf("state=%#v err=%v", state, err)
	}
}
