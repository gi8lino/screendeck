package store

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gi8lino/screendeck/internal/plex"
)

// TestUnanimousMatchLifecycle verifies matching across all room participants.
func TestUnanimousMatchLifecycle(t *testing.T) {
	ctx := context.Background()
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	movie := plex.Item{RatingKey: "42", Library: "1", Type: "movie", Title: "Arrival", Genres: []string{"Science Fiction"}}
	if err := database.SaveLibrary(ctx, plex.Library{Key: "1", Title: "Films"}, []plex.Item{movie}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := database.CreateRoom(ctx, Room{Code: "ABC123", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, Participant{ID: "p1", Name: "One"}, "hash1", []string{"42"}); err != nil {
		t.Fatal(err)
	}
	if err := database.JoinRoom(ctx, "ABC123", Participant{ID: "p2", Name: "Two"}, "hash2"); err != nil {
		t.Fatal(err)
	}
	matched, err := database.Vote(ctx, "ABC123", "p1", "42", true)
	if err != nil || matched {
		t.Fatalf("first vote matched=%v err=%v", matched, err)
	}
	matched, err = database.Vote(ctx, "ABC123", "p2", "42", true)
	if err != nil || !matched {
		t.Fatalf("second vote matched=%v err=%v", matched, err)
	}
	state, err := database.RoomState(ctx, "ABC123", "p2")
	if err != nil {
		t.Fatal(err)
	}
	if state.Candidate != nil || len(state.Matches) != 1 || state.Progress.Voted != 1 {
		t.Fatalf("unexpected state: %#v", state)
	}
}

// TestPlexAuthenticationIsEncryptedAtRest verifies stored Plex secrets are encrypted.
func TestPlexAuthenticationIsEncryptedAtRest(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	keyPath := filepath.Join(directory, "auth.key")
	database, err := Open(filepath.Join(directory, "test.db"), keyPath)
	if err != nil {
		t.Fatal(err)
	}
	_, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	state := plex.AuthState{
		Method: plex.AuthMethodJWT, ClientID: "client", KeyID: "key", PrivateKey: privateKey, UserToken: "secret-user-token",
		TokenExpiresAt: time.Now().Add(time.Hour), ServerID: "server", ServerName: "Plex",
		ServerURL: "http://plex.test:32400", ServerToken: "secret-server-token",
	}
	if err := database.SavePlexAuth(ctx, state); err != nil {
		t.Fatal(err)
	}
	loaded, err := database.LoadPlexAuth(ctx)
	if err != nil || loaded.Method != plex.AuthMethodJWT || loaded.UserToken != state.UserToken || loaded.ServerToken != state.ServerToken || !bytes.Equal(loaded.PrivateKey, state.PrivateKey) {
		t.Fatalf("round trip state=%#v err=%v", loaded, err)
	}
	var encryptedPrivate, encryptedUser, encryptedServer []byte
	if err := database.db.QueryRow(`SELECT private_key,user_token,server_token FROM plex_auth WHERE id=1`).Scan(&encryptedPrivate, &encryptedUser, &encryptedServer); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encryptedPrivate, privateKey) || bytes.Contains(encryptedUser, []byte(state.UserToken)) || bytes.Contains(encryptedServer, []byte(state.ServerToken)) {
		t.Fatal("authentication secrets were stored in plaintext")
	}
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key permissions=%v", info.Mode().Perm())
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(filepath.Join(directory, "test.db"), keyPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	reloaded, err := reopened.LoadPlexAuth(ctx)
	if err != nil || reloaded.UserToken != state.UserToken || !bytes.Equal(reloaded.PrivateKey, state.PrivateKey) {
		t.Fatalf("reloaded state=%#v err=%v", reloaded, err)
	}
}

// TestLegacyPlexAuthenticationRoundTrip verifies legacy state does not require a device key.
func TestLegacyPlexAuthenticationRoundTrip(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	state := plex.AuthState{
		Method: plex.AuthMethodLegacy, ClientID: "client", UserToken: "legacy-user-token",
		ServerID: "server", ServerName: "Plex", ServerURL: "http://plex.test:32400", ServerToken: "legacy-server-token",
	}
	if err := database.SavePlexAuth(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	loaded, err := database.LoadPlexAuth(context.Background())
	if err != nil || loaded.Method != plex.AuthMethodLegacy || len(loaded.PrivateKey) != 0 || loaded.UserToken != state.UserToken || loaded.ServerToken != state.ServerToken {
		t.Fatalf("round trip state=%#v err=%v", loaded, err)
	}
}

// TestLeavingParticipantCanCompleteMatch verifies departed participants no longer block matches.
func TestLeavingParticipantCanCompleteMatch(t *testing.T) {
	ctx := context.Background()
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	movie := plex.Item{RatingKey: "7", Library: "1", Type: "movie", Title: "Alien"}
	if err := database.SaveLibrary(ctx, plex.Library{Key: "1", Title: "Films"}, []plex.Item{movie}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := database.CreateRoom(ctx, Room{Code: "LEAVE1", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, Participant{ID: "p1", Name: "One"}, "hash1", []string{"7"}); err != nil {
		t.Fatal(err)
	}
	for _, participant := range []struct{ id, name, hash string }{{"p2", "Two", "hash2"}, {"p3", "Three", "hash3"}} {
		if err := database.JoinRoom(ctx, "LEAVE1", Participant{ID: participant.id, Name: participant.name}, participant.hash); err != nil {
			t.Fatal(err)
		}
	}
	for _, participantID := range []string{"p1", "p2"} {
		if _, err := database.Vote(ctx, "LEAVE1", participantID, "7", true); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.LeaveRoom(ctx, "LEAVE1", "hash3"); err != nil {
		t.Fatal(err)
	}
	state, err := database.RoomState(ctx, "LEAVE1", "p1")
	if err != nil || len(state.Matches) != 1 || len(state.Participants) != 2 {
		t.Fatalf("state after leave = %#v, %v", state, err)
	}
}

// TestAdvanceRoundNarrowsMatches verifies matches can repeatedly become the next deck until one title remains.
func TestAdvanceRoundNarrowsMatches(t *testing.T) {
	ctx := context.Background()
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	movies := []plex.Item{
		{RatingKey: "a", Library: "1", Type: "movie", Title: "Alpha"},
		{RatingKey: "b", Library: "1", Type: "movie", Title: "Beta"},
		{RatingKey: "c", Library: "1", Type: "movie", Title: "Gamma"},
	}
	if err := database.SaveLibrary(ctx, plex.Library{Key: "1", Title: "Films"}, movies); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := database.CreateRoom(ctx, Room{Code: "ROUND1", Round: 1, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, Participant{ID: "p1", Name: "One"}, "hash1", []string{"a", "b", "c"}); err != nil {
		t.Fatal(err)
	}
	if err := database.JoinRoom(ctx, "ROUND1", Participant{ID: "p2", Name: "Two"}, "hash2"); err != nil {
		t.Fatal(err)
	}
	for _, participantID := range []string{"p1", "p2"} {
		for _, movieID := range []string{"a", "b", "c"} {
			if _, err := database.Vote(ctx, "ROUND1", participantID, movieID, true); err != nil {
				t.Fatal(err)
			}
		}
	}
	state, err := database.RoomState(ctx, "ROUND1", "p1")
	if err != nil || !state.RoundComplete || len(state.Matches) != 3 || state.Room.Round != 1 {
		t.Fatalf("round one state=%#v err=%v", state, err)
	}
	round, titles, advanced, err := database.AdvanceRound(ctx, "ROUND1", "p1", 1)
	if err != nil || !advanced || round != 2 || titles != 3 {
		t.Fatalf("advance round one: round=%d titles=%d advanced=%v err=%v", round, titles, advanced, err)
	}
	state, err = database.RoomState(ctx, "ROUND1", "p1")
	if err != nil || state.Room.Round != 2 || state.Progress.Voted != 0 || state.Progress.Total != 3 || len(state.Matches) != 0 {
		t.Fatalf("round two initial state=%#v err=%v", state, err)
	}
	for _, movieID := range []string{"a", "b", "c"} {
		if _, err := database.Vote(ctx, "ROUND1", "p1", movieID, true); err != nil {
			t.Fatal(err)
		}
	}
	for _, vote := range []struct {
		movieID string
		liked   bool
	}{{"a", true}, {"b", true}, {"c", false}} {
		if _, err := database.Vote(ctx, "ROUND1", "p2", vote.movieID, vote.liked); err != nil {
			t.Fatal(err)
		}
	}
	round, titles, advanced, err = database.AdvanceRound(ctx, "ROUND1", "p2", 2)
	if err != nil || !advanced || round != 3 || titles != 2 {
		t.Fatalf("advance round two: round=%d titles=%d advanced=%v err=%v", round, titles, advanced, err)
	}
	for _, movieID := range []string{"a", "b"} {
		if _, err := database.Vote(ctx, "ROUND1", "p1", movieID, true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.Vote(ctx, "ROUND1", "p2", "a", true); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Vote(ctx, "ROUND1", "p2", "b", false); err != nil {
		t.Fatal(err)
	}
	state, err = database.RoomState(ctx, "ROUND1", "p1")
	if err != nil || !state.RoundComplete || len(state.Matches) != 1 || state.Matches[0].RatingKey != "a" {
		t.Fatalf("final state=%#v err=%v", state, err)
	}
	if _, _, _, err := database.AdvanceRound(ctx, "ROUND1", "p1", 3); err == nil {
		t.Fatal("expected another round with one match to fail")
	}
}
