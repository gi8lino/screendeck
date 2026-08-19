package plex

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestClientLoadsLibrariesAndMovies verifies catalog loading from Plex.
func TestClientLoadsLibrariesAndMovies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Plex-Token") != "token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/library/sections":
			fmt.Fprint(w, `{"MediaContainer":{"Directory":[{"key":"1","title":"Films","type":"movie"},{"key":"2","title":"TV","type":"show"}]}}`)
		case "/library/sections/1/all":
			fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"42","guid":"plex://movie/42","title":"Arrival","year":2016,"summary":"First contact.","duration":6960000,"rating":7.9,"thumb":"/poster/42","viewCount":1,"Genre":[{"tag":"Science Fiction"}]}]}}`)
		case "/library/sections/2/all":
			fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"84","type":"show","guid":"plex://show/84","title":"Severance","year":2022,"thumb":"/poster/84","leafCount":19,"viewedLeafCount":19,"Genre":[{"tag":"Drama"}]}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := New(server.URL, "token")
	if err != nil {
		t.Fatal(err)
	}
	libraries, err := client.Libraries(context.Background())
	if err != nil || len(libraries) != 2 || libraries[0].Title != "Films" || libraries[1].Type != "show" {
		t.Fatalf("Libraries() = %#v, %v", libraries, err)
	}
	movies, err := client.Items(context.Background(), libraries[0])
	if err != nil || len(movies) != 1 {
		t.Fatalf("Movies() = %#v, %v", movies, err)
	}
	if movies[0].Title != "Arrival" || !movies[0].Viewed || len(movies[0].Genres) != 1 {
		t.Fatalf("unexpected movie: %#v", movies[0])
	}
	shows, err := client.Items(context.Background(), libraries[1])
	if err != nil || len(shows) != 1 || shows[0].Type != "show" || !shows[0].Viewed {
		t.Fatalf("unexpected shows: %#v, %v", shows, err)
	}
}

// TestClientRejectsInvalidLibraryKey verifies unsafe library keys are rejected.
func TestClientRejectsInvalidLibraryKey(t *testing.T) {
	client, _ := New("http://plex.test", "token")
	if _, err := client.Items(context.Background(), Library{Key: "../secret", Type: "movie"}); err == nil {
		t.Fatal("expected invalid library key error")
	}
}
