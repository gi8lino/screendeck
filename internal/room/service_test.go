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
	session, err := service.Create(context.Background(), "Host", []string{"1", "2"}, Filters{Genres: []string{"Drama"}, YearFrom: 2020, MaxDurationMinutes: 120}, nil)
	if err != nil {
		t.Fatal(err)
	}
	state, err := service.State(context.Background(), session.Code, session.Token)
	if err != nil || state.Progress.Total != 1 || state.Candidate == nil || state.Candidate.Type != "show" {
		t.Fatalf("state=%#v err=%v", state, err)
	}
}

// TestParticipantGenresFilterPersonalDecks verifies each participant can narrow only their own candidates.
func TestParticipantGenresFilterPersonalDecks(t *testing.T) {
	database, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	catalog := fakeCatalog{
		libraries: []plex.Library{{Key: "1", Title: "Films", Type: "movie"}},
		items: map[string][]plex.Item{
			"1": {
				{RatingKey: "action", Library: "1", Type: "movie", Title: "Action Pick", Genres: []string{"Action"}},
				{RatingKey: "drama", Library: "1", Type: "movie", Title: "Drama Pick", Genres: []string{"Drama"}},
			},
		},
	}
	service := NewService(database, catalog, time.Hour)
	host, err := service.Create(context.Background(), "Host", []string{"1"}, Filters{}, []string{"Drama"})
	if err != nil {
		t.Fatal(err)
	}
	hostState, err := service.State(context.Background(), host.Code, host.Token)
	if err != nil || hostState.Progress.Total != 1 || hostState.Candidate == nil || hostState.Candidate.RatingKey != "drama" {
		t.Fatalf("host state=%#v err=%v", hostState, err)
	}
	guest, err := service.Join(context.Background(), host.Code, "Guest", []string{"Action"})
	if err != nil {
		t.Fatal(err)
	}
	guestState, err := service.State(context.Background(), guest.Code, guest.Token)
	if err != nil || guestState.Progress.Total != 1 || guestState.Candidate == nil || guestState.Candidate.RatingKey != "action" {
		t.Fatalf("guest state=%#v err=%v", guestState, err)
	}
	if len(guestState.Me.Genres) != 1 || guestState.Me.Genres[0] != "Action" {
		t.Fatalf("guest genres=%#v", guestState.Me.Genres)
	}
}
