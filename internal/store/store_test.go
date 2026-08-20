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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUnanimousMatchLifecycle verifies matching across all room participants.
func TestUnanimousMatchLifecycle(t *testing.T) {
	ctx := context.Background()
	database, err := Open(":memory:")
	require.NoError(t, err)
	defer database.Close()

	movie := plex.Item{RatingKey: "42", Library: "1", Type: "movie", Title: "Arrival", Genres: []string{"Science Fiction"}}
	require.NoError(t, database.SaveLibrary(ctx, plex.Library{Key: "1", Title: "Films"}, []plex.Item{movie}))

	now := time.Now().UTC()
	require.NoError(t, database.CreateRoom(ctx, Room{Code: "ABC123", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, Participant{ID: "p1", Name: "One"}, "hash1", []string{"42"}))
	require.NoError(t, database.JoinRoom(ctx, "ABC123", Participant{ID: "p2", Name: "Two"}, "hash2"))

	matched, err := database.Vote(ctx, "ABC123", "p1", "42", true)
	require.NoError(t, err)
	assert.False(t, matched)

	matched, err = database.Vote(ctx, "ABC123", "p2", "42", true)
	require.NoError(t, err)
	assert.True(t, matched)

	state, err := database.RoomState(ctx, "ABC123", "p2")
	require.NoError(t, err)
	assert.Nil(t, state.Candidate)
	assert.Len(t, state.Matches, 1)
	assert.Equal(t, 1, state.Progress.Voted)
}

// TestPlexAuthenticationIsEncryptedAtRest verifies stored Plex secrets are encrypted.
func TestPlexAuthenticationIsEncryptedAtRest(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	keyPath := filepath.Join(directory, "auth.key")
	database, err := Open(filepath.Join(directory, "test.db"), keyPath)
	require.NoError(t, err)

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	state := plex.AuthState{
		Method: plex.AuthMethodJWT, ClientID: "client", KeyID: "key", PrivateKey: privateKey, UserToken: "secret-user-token",
		TokenExpiresAt: time.Now().Add(time.Hour), ServerID: "server", ServerName: "Plex",
		ServerURL: "http://plex.test:32400", ServerToken: "secret-server-token",
	}
	require.NoError(t, database.SavePlexAuth(ctx, state))

	loaded, err := database.LoadPlexAuth(ctx)
	require.NoError(t, err)
	assert.Equal(t, plex.AuthMethodJWT, loaded.Method)
	assert.Equal(t, state.UserToken, loaded.UserToken)
	assert.Equal(t, state.ServerToken, loaded.ServerToken)
	assert.Equal(t, state.PrivateKey, loaded.PrivateKey)

	var encryptedPrivate, encryptedUser, encryptedServer []byte
	require.NoError(t, database.db.QueryRow(`SELECT private_key,user_token,server_token FROM plex_auth WHERE id=1`).Scan(&encryptedPrivate, &encryptedUser, &encryptedServer))
	assert.False(t, bytes.Contains(encryptedPrivate, privateKey))
	assert.False(t, bytes.Contains(encryptedUser, []byte(state.UserToken)))
	assert.False(t, bytes.Contains(encryptedServer, []byte(state.ServerToken)))

	info, err := os.Stat(keyPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	require.NoError(t, database.Close())
	reopened, err := Open(filepath.Join(directory, "test.db"), keyPath)
	require.NoError(t, err)
	defer reopened.Close()

	reloaded, err := reopened.LoadPlexAuth(ctx)
	require.NoError(t, err)
	assert.Equal(t, state.UserToken, reloaded.UserToken)
	assert.Equal(t, state.PrivateKey, reloaded.PrivateKey)
}

// TestLegacyPlexAuthenticationRoundTrip verifies legacy state does not require a device key.
func TestLegacyPlexAuthenticationRoundTrip(t *testing.T) {
	database, err := Open(":memory:")
	require.NoError(t, err)
	defer database.Close()

	state := plex.AuthState{
		Method: plex.AuthMethodLegacy, ClientID: "client", UserToken: "legacy-user-token",
		ServerID: "server", ServerName: "Plex", ServerURL: "http://plex.test:32400", ServerToken: "legacy-server-token",
	}
	require.NoError(t, database.SavePlexAuth(context.Background(), state))

	loaded, err := database.LoadPlexAuth(context.Background())
	require.NoError(t, err)
	assert.Equal(t, plex.AuthMethodLegacy, loaded.Method)
	assert.Empty(t, loaded.PrivateKey)
	assert.Equal(t, state.UserToken, loaded.UserToken)
	assert.Equal(t, state.ServerToken, loaded.ServerToken)
}

// TestLeavingParticipantCanCompleteMatch verifies departed participants no longer block matches.
func TestLeavingParticipantCanCompleteMatch(t *testing.T) {
	ctx := context.Background()
	database, err := Open(":memory:")
	require.NoError(t, err)
	defer database.Close()

	movie := plex.Item{RatingKey: "7", Library: "1", Type: "movie", Title: "Alien"}
	require.NoError(t, database.SaveLibrary(ctx, plex.Library{Key: "1", Title: "Films"}, []plex.Item{movie}))

	now := time.Now().UTC()
	require.NoError(t, database.CreateRoom(ctx, Room{Code: "LEAVE1", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, Participant{ID: "p1", Name: "One"}, "hash1", []string{"7"}))
	require.NoError(t, database.JoinRoom(ctx, "LEAVE1", Participant{ID: "p2", Name: "Two"}, "hash2"))
	require.NoError(t, database.JoinRoom(ctx, "LEAVE1", Participant{ID: "p3", Name: "Three"}, "hash3"))

	_, err = database.Vote(ctx, "LEAVE1", "p1", "7", true)
	require.NoError(t, err)
	_, err = database.Vote(ctx, "LEAVE1", "p2", "7", true)
	require.NoError(t, err)
	require.NoError(t, database.LeaveRoom(ctx, "LEAVE1", "hash3"))

	state, err := database.RoomState(ctx, "LEAVE1", "p1")
	require.NoError(t, err)
	assert.Len(t, state.Matches, 1)
	assert.Len(t, state.Participants, 2)
}

// TestRoundReadinessNarrowsBeforeDeckCompletion verifies unanimous readiness can advance a round early.
func TestRoundReadinessNarrowsBeforeDeckCompletion(t *testing.T) {
	ctx := context.Background()
	database, err := Open(":memory:")
	require.NoError(t, err)
	defer database.Close()

	movies := []plex.Item{
		{RatingKey: "a", Library: "1", Type: "movie", Title: "Alpha"},
		{RatingKey: "b", Library: "1", Type: "movie", Title: "Beta"},
		{RatingKey: "c", Library: "1", Type: "movie", Title: "Gamma"},
	}
	require.NoError(t, database.SaveLibrary(ctx, plex.Library{Key: "1", Title: "Films"}, movies))

	now := time.Now().UTC()
	require.NoError(t, database.CreateRoom(ctx, Room{Code: "ROUND1", Round: 1, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, Participant{ID: "p1", Name: "One"}, "hash1", []string{"a", "b", "c"}))
	require.NoError(t, database.JoinRoom(ctx, "ROUND1", Participant{ID: "p2", Name: "Two"}, "hash2"))

	_, err = database.Vote(ctx, "ROUND1", "p1", "a", true)
	require.NoError(t, err)
	_, err = database.Vote(ctx, "ROUND1", "p2", "a", true)
	require.NoError(t, err)
	_, err = database.Vote(ctx, "ROUND1", "p1", "b", true)
	require.NoError(t, err)
	_, err = database.Vote(ctx, "ROUND1", "p2", "b", true)
	require.NoError(t, err)

	state, err := database.RoomState(ctx, "ROUND1", "p1")
	require.NoError(t, err)
	assert.False(t, state.RoundComplete)
	assert.Len(t, state.Matches, 2)
	assert.True(t, state.NextRound.Available)
	assert.Equal(t, 0, state.NextRound.Ready)

	round, titles, ready, required, advanced, err := database.SetRoundReady(ctx, "ROUND1", "p1", 1, true)
	require.NoError(t, err)
	assert.False(t, advanced)
	assert.Equal(t, 1, round)
	assert.Equal(t, 0, titles)
	assert.Equal(t, 1, ready)
	assert.Equal(t, 2, required)

	state, err = database.RoomState(ctx, "ROUND1", "p1")
	require.NoError(t, err)
	assert.True(t, state.Me.ReadyForNextRound)
	assert.Equal(t, 1, state.NextRound.Ready)

	_, _, ready, required, advanced, err = database.SetRoundReady(ctx, "ROUND1", "p1", 1, false)
	require.NoError(t, err)
	assert.False(t, advanced)
	assert.Equal(t, 0, ready)
	assert.Equal(t, 2, required)

	_, _, ready, required, advanced, err = database.SetRoundReady(ctx, "ROUND1", "p1", 1, true)
	require.NoError(t, err)
	assert.False(t, advanced)
	assert.Equal(t, 1, ready)
	assert.Equal(t, 2, required)

	round, titles, ready, required, advanced, err = database.SetRoundReady(ctx, "ROUND1", "p2", 1, true)
	require.NoError(t, err)
	assert.True(t, advanced)
	assert.Equal(t, 2, round)
	assert.Equal(t, 2, titles)
	assert.Equal(t, 2, ready)
	assert.Equal(t, 2, required)

	state, err = database.RoomState(ctx, "ROUND1", "p1")
	require.NoError(t, err)
	assert.Equal(t, 2, state.Room.Round)
	assert.Equal(t, 0, state.Progress.Voted)
	assert.Equal(t, 2, state.Progress.Total)
	assert.Empty(t, state.Matches)
	assert.Equal(t, 0, state.NextRound.Ready)
	assert.False(t, state.Me.ReadyForNextRound)

	_, err = database.Vote(ctx, "ROUND1", "p1", "a", true)
	require.NoError(t, err)
	_, err = database.Vote(ctx, "ROUND1", "p2", "a", true)
	require.NoError(t, err)
	_, err = database.Vote(ctx, "ROUND1", "p1", "b", true)
	require.NoError(t, err)
	_, err = database.Vote(ctx, "ROUND1", "p2", "b", false)
	require.NoError(t, err)

	state, err = database.RoomState(ctx, "ROUND1", "p1")
	require.NoError(t, err)
	assert.True(t, state.RoundComplete)
	require.Len(t, state.Matches, 1)
	assert.Equal(t, "a", state.Matches[0].RatingKey)

	_, _, _, _, _, err = database.SetRoundReady(ctx, "ROUND1", "p1", 2, true)
	require.Error(t, err)
}
