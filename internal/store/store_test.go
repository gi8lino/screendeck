package store

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
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
	defer database.Close() // nolint:errcheck

	item := plex.Item{RatingKey: "42", Library: "1", Type: "movie", Title: "Arrival", Genres: []string{"Science Fiction"}}
	require.NoError(t, database.SaveLibrary(ctx, plex.Library{Key: "1", Title: "Films"}, []plex.Item{item}))

	now := time.Now().UTC()
	require.NoError(t, database.CreateRoom(ctx, Room{Code: "ABC123", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, Participant{ID: "p1", Name: "One"}, "hash1", []string{"42"}, []string{"42"}))
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
	assert.Equal(t, RoomPhaseFinished, state.Room.Phase)
	assert.Len(t, state.Matches, 1)
	require.NotNil(t, state.Winner)
	assert.Equal(t, "42", state.Winner.Item.RatingKey)
	assert.Len(t, state.Winner.LikedBy, 2)
	assert.Equal(t, "One", state.Winner.LikedBy[0].Name)
	assert.Equal(t, "Two", state.Winner.LikedBy[1].Name)
	assert.True(t, state.Participants[0].IsHost)
	assert.False(t, state.Participants[1].IsHost)
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
	const encryptedAuthQuery = `
SELECT
  private_key,
  user_token,
  server_token
FROM plex_auth
WHERE id = 1
`
	require.NoError(t, database.db.QueryRow(encryptedAuthQuery).Scan(&encryptedPrivate, &encryptedUser, &encryptedServer))
	assert.False(t, bytes.Contains(encryptedPrivate, privateKey))
	assert.False(t, bytes.Contains(encryptedUser, []byte(state.UserToken)))
	assert.False(t, bytes.Contains(encryptedServer, []byte(state.ServerToken)))

	info, err := os.Stat(keyPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	require.NoError(t, database.Close())
	reopened, err := Open(filepath.Join(directory, "test.db"), keyPath)
	require.NoError(t, err)
	defer reopened.Close() // nolint:errcheck

	reloaded, err := reopened.LoadPlexAuth(ctx)
	require.NoError(t, err)
	assert.Equal(t, state.UserToken, reloaded.UserToken)
	assert.Equal(t, state.PrivateKey, reloaded.PrivateKey)
}

// TestLegacyPlexAuthenticationRoundTrip verifies legacy state does not require a device key.
func TestLegacyPlexAuthenticationRoundTrip(t *testing.T) {
	database, err := Open(":memory:")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

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
	defer database.Close() // nolint:errcheck

	item := plex.Item{RatingKey: "7", Library: "1", Type: "movie", Title: "Alien"}
	require.NoError(t, database.SaveLibrary(ctx, plex.Library{Key: "1", Title: "Films"}, []plex.Item{item}))

	now := time.Now().UTC()
	require.NoError(t, database.CreateRoom(ctx, Room{Code: "LEAVE1", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, Participant{ID: "p1", Name: "One"}, "hash1", []string{"7"}, []string{"7"}))
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
	defer database.Close() // nolint:errcheck

	items := []plex.Item{
		{RatingKey: "a", Library: "1", Type: "movie", Title: "Alpha"},
		{RatingKey: "b", Library: "1", Type: "movie", Title: "Beta"},
		{RatingKey: "c", Library: "1", Type: "movie", Title: "Gamma"},
	}
	require.NoError(t, database.SaveLibrary(ctx, plex.Library{Key: "1", Title: "Films"}, items))

	now := time.Now().UTC()
	require.NoError(t, database.CreateRoom(ctx, Room{Code: "ROUND1", Round: 1, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, Participant{ID: "p1", Name: "One"}, "hash1", []string{"a", "b", "c"}, []string{"a", "b", "c"}))
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
	assert.Equal(t, RoomPhaseSwiping, state.Room.Phase)
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
	assert.Equal(t, RoomPhaseNextRoundRequested, state.Room.Phase)
	assert.Equal(t, 1, state.NextRound.Ready)
	require.NotNil(t, state.NextRound.RequestedBy)
	assert.Equal(t, "p1", state.NextRound.RequestedBy.ID)
	assert.Equal(t, "One", state.NextRound.RequestedBy.Name)

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
	assert.Equal(t, RoomPhaseSwiping, state.Room.Phase)
	assert.Equal(t, 0, state.Progress.Voted)
	assert.Equal(t, 2, state.Progress.Total)
	assert.Empty(t, state.Matches)
	assert.Equal(t, 0, state.NextRound.Ready)
	assert.Nil(t, state.NextRound.RequestedBy)
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
	assert.Equal(t, RoomPhaseFinished, state.Room.Phase)
	require.Len(t, state.Matches, 1)
	assert.Equal(t, "a", state.Matches[0].RatingKey)

	_, _, _, _, _, err = database.SetRoundReady(ctx, "ROUND1", "p1", 2, true)
	require.Error(t, err)
}

// TestAddMoreTitlesUsesUnusedPool verifies first-round expansion is duplicate-free and bounded by the original pool.
func TestAddMoreTitlesUsesUnusedPool(t *testing.T) {
	ctx := context.Background()
	database, err := Open(":memory:")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	items := []plex.Item{
		{RatingKey: "a", Library: "1", Type: "movie", Title: "Alpha"},
		{RatingKey: "b", Library: "1", Type: "movie", Title: "Beta"},
		{RatingKey: "c", Library: "1", Type: "movie", Title: "Gamma"},
	}
	require.NoError(t, database.SaveLibrary(ctx, plex.Library{Key: "1", Title: "Films"}, items))
	now := time.Now().UTC()
	require.NoError(t, database.CreateRoom(ctx, Room{Code: "MORE01", Round: 1, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, Participant{ID: "p1", Name: "One"}, "hash1", []string{"a"}, []string{"a", "b", "c"}))

	state, err := database.RoomState(ctx, "MORE01", "p1")
	require.NoError(t, err)
	assert.Equal(t, 1, state.Progress.Total)
	assert.Equal(t, 2, state.MoreTitles.Available)
	assert.True(t, state.MoreTitles.CanAdd)

	added, remaining, err := database.AddMoreTitles(ctx, "MORE01", "p1", 1)
	require.NoError(t, err)
	assert.Equal(t, 1, added)
	assert.Equal(t, 1, remaining)

	state, err = database.RoomState(ctx, "MORE01", "p1")
	require.NoError(t, err)
	assert.Equal(t, 2, state.Progress.Total)
	assert.Equal(t, 1, state.MoreTitles.Available)
}

// TestNextRoundRequestCancelsWhenMatchesDropBelowTwo verifies stale readiness cannot survive an invalid match set.
func TestNextRoundRequestCancelsWhenMatchesDropBelowTwo(t *testing.T) {
	ctx := context.Background()
	database, err := Open(":memory:")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	items := []plex.Item{
		{RatingKey: "a", Library: "1", Type: "movie", Title: "Alpha"},
		{RatingKey: "b", Library: "1", Type: "movie", Title: "Beta"},
	}
	require.NoError(t, database.SaveLibrary(ctx, plex.Library{Key: "1", Title: "Films"}, items))
	now := time.Now().UTC()
	require.NoError(t, database.CreateRoom(ctx, Room{Code: "CANCEL", Round: 1, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, Participant{ID: "p1", Name: "One"}, "hash1", []string{"a", "b"}, []string{"a", "b"}))
	require.NoError(t, database.JoinRoom(ctx, "CANCEL", Participant{ID: "p2", Name: "Two"}, "hash2"))
	_, err = database.Vote(ctx, "CANCEL", "p1", "a", true)
	require.NoError(t, err)
	_, err = database.Vote(ctx, "CANCEL", "p2", "a", true)
	require.NoError(t, err)
	_, err = database.Vote(ctx, "CANCEL", "p1", "b", true)
	require.NoError(t, err)
	_, err = database.Vote(ctx, "CANCEL", "p2", "b", true)
	require.NoError(t, err)
	_, _, _, _, _, err = database.SetRoundReady(ctx, "CANCEL", "p1", 1, true)
	require.NoError(t, err)

	_, err = database.Vote(ctx, "CANCEL", "p1", "b", false)
	require.NoError(t, err)
	state, err := database.RoomState(ctx, "CANCEL", "p1")
	require.NoError(t, err)
	assert.Equal(t, 0, state.NextRound.Ready)
	assert.Nil(t, state.NextRound.RequestedBy)
	assert.Equal(t, RoomPhaseFinished, state.Room.Phase)
}

// TestMembershipChangeCancelsNextRoundRequest verifies the active group must consent again after a join.
func TestMembershipChangeCancelsNextRoundRequest(t *testing.T) {
	ctx := context.Background()
	database, err := Open(":memory:")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	items := []plex.Item{
		{RatingKey: "a", Library: "1", Type: "movie", Title: "Alpha"},
		{RatingKey: "b", Library: "1", Type: "movie", Title: "Beta"},
	}
	require.NoError(t, database.SaveLibrary(ctx, plex.Library{Key: "1", Title: "Films"}, items))
	now := time.Now().UTC()
	require.NoError(t, database.CreateRoom(ctx, Room{Code: "MEMBER", Round: 1, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, Participant{ID: "p1", Name: "One"}, "hash1", []string{"a", "b"}, []string{"a", "b"}))
	require.NoError(t, database.JoinRoom(ctx, "MEMBER", Participant{ID: "p2", Name: "Two"}, "hash2"))
	_, err = database.Vote(ctx, "MEMBER", "p1", "a", true)
	require.NoError(t, err)
	_, err = database.Vote(ctx, "MEMBER", "p2", "a", true)
	require.NoError(t, err)
	_, err = database.Vote(ctx, "MEMBER", "p1", "b", true)
	require.NoError(t, err)
	_, err = database.Vote(ctx, "MEMBER", "p2", "b", true)
	require.NoError(t, err)
	_, _, _, _, _, err = database.SetRoundReady(ctx, "MEMBER", "p1", 1, true)
	require.NoError(t, err)

	require.NoError(t, database.JoinRoom(ctx, "MEMBER", Participant{ID: "p3", Name: "Three"}, "hash3"))
	state, err := database.RoomState(ctx, "MEMBER", "p1")
	require.NoError(t, err)
	assert.Equal(t, 0, state.NextRound.Ready)
	assert.Nil(t, state.NextRound.RequestedBy)
	assert.Equal(t, RoomPhaseSwiping, state.Room.Phase)
}

// TestHostOwnershipTransfersOnLeave verifies the earliest remaining participant becomes host.
func TestHostOwnershipTransfersOnLeave(t *testing.T) {
	ctx := context.Background()
	database, err := Open(":memory:")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	item := plex.Item{RatingKey: "a", Library: "1", Type: "movie", Title: "Alpha"}
	require.NoError(t, database.SaveLibrary(ctx, plex.Library{Key: "1", Title: "Films"}, []plex.Item{item}))
	now := time.Now().UTC()
	require.NoError(t, database.CreateRoom(ctx, Room{Code: "HOST01", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, Participant{ID: "p1", Name: "Host"}, "hash1", []string{"a"}, []string{"a"}))
	require.NoError(t, database.JoinRoom(ctx, "HOST01", Participant{ID: "p2", Name: "Next"}, "hash2"))

	state, err := database.RoomState(ctx, "HOST01", "p1")
	require.NoError(t, err)
	assert.Equal(t, "p1", state.Room.OwnerID)
	assert.True(t, state.Me.IsHost)

	require.NoError(t, database.LeaveRoom(ctx, "HOST01", "hash1"))
	state, err = database.RoomState(ctx, "HOST01", "p2")
	require.NoError(t, err)
	assert.Equal(t, "p2", state.Room.OwnerID)
	assert.True(t, state.Me.IsHost)
}

// TestLegacyMediaSchemaMigration verifies movie-named persistence is migrated once to item terminology.
func TestLegacyMediaSchemaMigration(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "legacy.db")
	legacy, err := sql.Open("sqlite", databasePath)
	require.NoError(t, err)
	const legacySchema = `
CREATE TABLE libraries (
  key TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  synced_at INTEGER NOT NULL
);

CREATE TABLE movies (
  rating_key TEXT PRIMARY KEY,
  library_key TEXT NOT NULL,
  guid TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL,
  year INTEGER NOT NULL DEFAULT 0,
  summary TEXT NOT NULL DEFAULT '',
  duration INTEGER NOT NULL DEFAULT 0,
  rating REAL NOT NULL DEFAULT 0,
  thumb TEXT NOT NULL DEFAULT '',
  genres TEXT NOT NULL DEFAULT '[]',
  viewed INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE rooms (
  code TEXT PRIMARY KEY,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL
);

CREATE TABLE room_movies (
  room_code TEXT NOT NULL,
  movie_id TEXT NOT NULL,
  position INTEGER NOT NULL,
  PRIMARY KEY (room_code, movie_id)
);

CREATE TABLE participants (
  id TEXT PRIMARY KEY,
  room_code TEXT NOT NULL,
  name TEXT NOT NULL,
  token_hash TEXT NOT NULL UNIQUE,
  joined_at INTEGER NOT NULL
);

CREATE TABLE votes (
  room_code TEXT NOT NULL,
  participant_id TEXT NOT NULL,
  movie_id TEXT NOT NULL,
  liked INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (room_code, participant_id, movie_id)
);

CREATE TABLE matches (
  room_code TEXT NOT NULL,
  movie_id TEXT NOT NULL,
  matched_at INTEGER NOT NULL,
  PRIMARY KEY (room_code, movie_id)
);

INSERT INTO libraries (key, title, synced_at)
VALUES ('1', 'Films', 1);

INSERT INTO movies (rating_key, library_key, title, genres)
VALUES ('42', '1', 'Arrival', '["Science Fiction"]');

INSERT INTO rooms (code, created_at, expires_at)
VALUES ('LEGACY', 1, 4102444800);

INSERT INTO room_movies (room_code, movie_id, position)
VALUES ('LEGACY', '42', 0);

INSERT INTO participants (id, room_code, name, token_hash, joined_at)
VALUES ('p1', 'LEGACY', 'One', 'hash1', 1);

INSERT INTO votes (room_code, participant_id, movie_id, liked, created_at)
VALUES ('LEGACY', 'p1', '42', 1, 1);

INSERT INTO matches (room_code, movie_id, matched_at)
VALUES ('LEGACY', '42', 1);
`
	_, err = legacy.Exec(legacySchema)
	require.NoError(t, err)
	require.NoError(t, legacy.Close())

	database, err := Open(databasePath, filepath.Join(directory, "auth.key"))
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	var mediaItems, roomItems, votes, matches int
	const mediaItemCountQuery = `
SELECT COUNT(*)
FROM media_items
`
	require.NoError(t, database.db.QueryRow(mediaItemCountQuery).Scan(&mediaItems))
	const roomItemCountQuery = `
SELECT COUNT(*)
FROM room_items
`
	require.NoError(t, database.db.QueryRow(roomItemCountQuery).Scan(&roomItems))
	const voteCountQuery = `
SELECT COUNT(*)
FROM item_votes
`
	require.NoError(t, database.db.QueryRow(voteCountQuery).Scan(&votes))
	const matchCountQuery = `
SELECT COUNT(*)
FROM item_matches
`
	require.NoError(t, database.db.QueryRow(matchCountQuery).Scan(&matches))
	assert.Equal(t, 1, mediaItems)
	assert.Equal(t, 1, roomItems)
	assert.Equal(t, 1, votes)
	assert.Equal(t, 1, matches)

	moviesExists, err := database.tableExists(ctx, "movies")
	require.NoError(t, err)
	roomMoviesExists, err := database.tableExists(ctx, "room_movies")
	require.NoError(t, err)
	votesExists, err := database.tableExists(ctx, "votes")
	require.NoError(t, err)
	matchesExists, err := database.tableExists(ctx, "matches")
	require.NoError(t, err)
	assert.False(t, moviesExists)
	assert.False(t, roomMoviesExists)
	assert.False(t, votesExists)
	assert.False(t, matchesExists)

	state, err := database.RoomState(ctx, "LEGACY", "p1")
	require.NoError(t, err)
	assert.Len(t, state.Matches, 1)
	assert.Equal(t, "Arrival", state.Matches[0].Title)
	assert.True(t, state.Me.IsHost)
}

// TestConcurrentFinalVotesCreateOneMatch verifies simultaneous likes cannot create duplicate match state.
func TestConcurrentFinalVotesCreateOneMatch(t *testing.T) {
	ctx := context.Background()
	database, err := Open(":memory:")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	item := plex.Item{RatingKey: "a", Library: "1", Type: "movie", Title: "Alpha"}
	require.NoError(t, database.SaveLibrary(ctx, plex.Library{Key: "1", Title: "Films"}, []plex.Item{item}))
	now := time.Now().UTC()
	require.NoError(t, database.CreateRoom(ctx, Room{Code: "RACE01", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, Participant{ID: "p1", Name: "One"}, "hash1", []string{"a"}, []string{"a"}))
	require.NoError(t, database.JoinRoom(ctx, "RACE01", Participant{ID: "p2", Name: "Two"}, "hash2"))

	// voteResult captures the outcome of a concurrent vote in tests.
	type voteResult struct {
		// matched stores the matched value.
		matched bool
		// err stores the err value.
		err error
	}
	start := make(chan struct{})
	results := make(chan voteResult, 2)
	go func() {
		<-start
		matched, voteErr := database.Vote(ctx, "RACE01", "p1", "a", true)
		results <- voteResult{matched: matched, err: voteErr}
	}()
	go func() {
		<-start
		matched, voteErr := database.Vote(ctx, "RACE01", "p2", "a", true)
		results <- voteResult{matched: matched, err: voteErr}
	}()
	close(start)
	first := <-results
	second := <-results

	require.NoError(t, first.err)
	require.NoError(t, second.err)
	assert.NotEqual(t, first.matched, second.matched)
	state, err := database.RoomState(ctx, "RACE01", "p1")
	require.NoError(t, err)
	assert.Len(t, state.Matches, 1)
	assert.Equal(t, RoomPhaseFinished, state.Room.Phase)
}

// TestConcurrentReadinessAdvancesExactlyOneRound verifies simultaneous consent cannot skip or duplicate a round.
func TestConcurrentReadinessAdvancesExactlyOneRound(t *testing.T) {
	ctx := context.Background()
	database := seedReadyConcurrencyRoom(t, ctx, "RACE02")
	defer database.Close() // nolint:errcheck

	// readyResult captures the outcome of a concurrent readiness update in tests.
	type readyResult struct {
		// round stores the round value.
		round int
		// advanced stores the advanced value.
		advanced bool
		// err stores the err value.
		err error
	}
	start := make(chan struct{})
	results := make(chan readyResult, 2)
	go func() {
		<-start
		round, _, _, _, advanced, readyErr := database.SetRoundReady(ctx, "RACE02", "p1", 1, true)
		results <- readyResult{round: round, advanced: advanced, err: readyErr}
	}()
	go func() {
		<-start
		round, _, _, _, advanced, readyErr := database.SetRoundReady(ctx, "RACE02", "p2", 1, true)
		results <- readyResult{round: round, advanced: advanced, err: readyErr}
	}()
	close(start)
	first := <-results
	second := <-results

	require.NoError(t, first.err)
	require.NoError(t, second.err)
	assert.True(t, first.advanced != second.advanced)
	state, err := database.RoomState(ctx, "RACE02", "p1")
	require.NoError(t, err)
	assert.Equal(t, 2, state.Room.Round)
	assert.Equal(t, 2, state.Progress.RoundTotal)
	assert.Equal(t, 0, state.NextRound.Ready)
}

// TestConcurrentDuplicateReadinessIsIdempotent verifies duplicate ready submissions count a participant once.
func TestConcurrentDuplicateReadinessIsIdempotent(t *testing.T) {
	ctx := context.Background()
	database := seedReadyConcurrencyRoom(t, ctx, "RACE03")
	defer database.Close() // nolint:errcheck

	// readyResult captures the outcome of a concurrent readiness update in tests.
	type readyResult struct {
		// ready stores the ready value.
		ready int
		// err stores the err value.
		err error
	}
	start := make(chan struct{})
	results := make(chan readyResult, 2)
	go func() {
		<-start
		_, _, ready, _, _, readyErr := database.SetRoundReady(ctx, "RACE03", "p1", 1, true)
		results <- readyResult{ready: ready, err: readyErr}
	}()
	go func() {
		<-start
		_, _, ready, _, _, readyErr := database.SetRoundReady(ctx, "RACE03", "p1", 1, true)
		results <- readyResult{ready: ready, err: readyErr}
	}()
	close(start)
	first := <-results
	second := <-results

	require.NoError(t, first.err)
	require.NoError(t, second.err)
	assert.Equal(t, 1, first.ready)
	assert.Equal(t, 1, second.ready)
	state, err := database.RoomState(ctx, "RACE03", "p1")
	require.NoError(t, err)
	assert.Equal(t, 1, state.NextRound.Ready)
	assert.Equal(t, RoomPhaseNextRoundRequested, state.Room.Phase)
}

// TestConcurrentFinalReadyRequestIsIdempotent verifies two final submissions cannot advance twice.
func TestConcurrentFinalReadyRequestIsIdempotent(t *testing.T) {
	ctx := context.Background()
	database := seedReadyConcurrencyRoom(t, ctx, "RACE04")
	defer database.Close() // nolint:errcheck
	_, _, _, _, advanced, err := database.SetRoundReady(ctx, "RACE04", "p1", 1, true)
	require.NoError(t, err)
	assert.False(t, advanced)

	// readyResult captures the outcome of a concurrent readiness update in tests.
	type readyResult struct {
		// round stores the round value.
		round int
		// advanced stores the advanced value.
		advanced bool
		// err stores the err value.
		err error
	}
	start := make(chan struct{})
	results := make(chan readyResult, 2)
	go func() {
		<-start
		round, _, _, _, didAdvance, readyErr := database.SetRoundReady(ctx, "RACE04", "p2", 1, true)
		results <- readyResult{round: round, advanced: didAdvance, err: readyErr}
	}()
	go func() {
		<-start
		round, _, _, _, didAdvance, readyErr := database.SetRoundReady(ctx, "RACE04", "p2", 1, true)
		results <- readyResult{round: round, advanced: didAdvance, err: readyErr}
	}()
	close(start)
	first := <-results
	second := <-results

	require.NoError(t, first.err)
	require.NoError(t, second.err)
	assert.True(t, first.advanced)
	assert.True(t, second.advanced)
	assert.Equal(t, 2, first.round)
	assert.Equal(t, 2, second.round)
	state, err := database.RoomState(ctx, "RACE04", "p1")
	require.NoError(t, err)
	assert.Equal(t, 2, state.Room.Round)
	assert.Equal(t, 2, state.Progress.RoundTotal)
}

// TestReadyParticipantDepartureCancelsConsensus verifies membership changes cannot inherit stale readiness.
func TestReadyParticipantDepartureCancelsConsensus(t *testing.T) {
	ctx := context.Background()
	database := seedReadyConcurrencyRoom(t, ctx, "RACE05")
	defer database.Close() // nolint:errcheck
	require.NoError(t, database.JoinRoom(ctx, "RACE05", Participant{ID: "p3", Name: "Three"}, "hash3"))

	_, _, _, _, _, err := database.SetRoundReady(ctx, "RACE05", "p1", 1, true)
	require.Error(t, err)
	// The new participant invalidated existing matches, so recreate two unanimous matches.
	_, err = database.Vote(ctx, "RACE05", "p3", "a", true)
	require.NoError(t, err)
	_, err = database.Vote(ctx, "RACE05", "p3", "b", true)
	require.NoError(t, err)
	_, _, _, _, _, err = database.SetRoundReady(ctx, "RACE05", "p1", 1, true)
	require.NoError(t, err)
	_, _, _, _, _, err = database.SetRoundReady(ctx, "RACE05", "p2", 1, true)
	require.NoError(t, err)

	require.NoError(t, database.LeaveRoom(ctx, "RACE05", "hash3"))
	state, err := database.RoomState(ctx, "RACE05", "p1")
	require.NoError(t, err)
	assert.Equal(t, 0, state.NextRound.Ready)
	assert.Nil(t, state.NextRound.RequestedBy)
	assert.Equal(t, 1, state.Room.Round)
}

// seedReadyConcurrencyRoom creates a two-person room with two unanimous matches.
func seedReadyConcurrencyRoom(t *testing.T, ctx context.Context, code string) *Store {
	t.Helper()
	database, err := Open(":memory:")
	require.NoError(t, err)
	items := []plex.Item{
		{RatingKey: "a", Library: "1", Type: "movie", Title: "Alpha"},
		{RatingKey: "b", Library: "1", Type: "movie", Title: "Beta"},
	}
	require.NoError(t, database.SaveLibrary(ctx, plex.Library{Key: "1", Title: "Films"}, items))
	now := time.Now().UTC()
	require.NoError(t, database.CreateRoom(ctx, Room{Code: code, Round: 1, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, Participant{ID: "p1", Name: "One"}, "hash1", []string{"a", "b"}, []string{"a", "b"}))
	require.NoError(t, database.JoinRoom(ctx, code, Participant{ID: "p2", Name: "Two"}, "hash2"))
	_, err = database.Vote(ctx, code, "p1", "a", true)
	require.NoError(t, err)
	_, err = database.Vote(ctx, code, "p2", "a", true)
	require.NoError(t, err)
	_, err = database.Vote(ctx, code, "p1", "b", true)
	require.NoError(t, err)
	_, err = database.Vote(ctx, code, "p2", "b", true)
	require.NoError(t, err)
	return database
}

// TestRemoveParticipant verifies host-only participant removal and room-state reconciliation.
func TestRemoveParticipant(t *testing.T) {
	t.Run("host can remove another participant and readiness resets", func(t *testing.T) {
		ctx := context.Background()
		database, err := Open(":memory:")
		require.NoError(t, err)
		defer database.Close() // nolint:errcheck

		items := []plex.Item{
			{RatingKey: "a", Library: "1", Type: "movie", Title: "Alpha"},
			{RatingKey: "b", Library: "1", Type: "movie", Title: "Beta"},
		}
		require.NoError(t, database.SaveLibrary(ctx, plex.Library{Key: "1", Title: "Films"}, items))
		now := time.Now().UTC()
		require.NoError(t, database.CreateRoom(ctx, Room{Code: "RMHOST", Round: 1, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, Participant{ID: "p1", Name: "Host"}, "hash1", []string{"a", "b"}, []string{"a", "b"}))
		require.NoError(t, database.JoinRoom(ctx, "RMHOST", Participant{ID: "p2", Name: "Guest"}, "hash2"))
		require.NoError(t, database.JoinRoom(ctx, "RMHOST", Participant{ID: "p3", Name: "Third"}, "hash3"))
		_, err = database.Vote(ctx, "RMHOST", "p1", "a", true)
		require.NoError(t, err)
		_, err = database.Vote(ctx, "RMHOST", "p2", "a", true)
		require.NoError(t, err)
		_, err = database.Vote(ctx, "RMHOST", "p3", "a", true)
		require.NoError(t, err)
		_, err = database.Vote(ctx, "RMHOST", "p1", "b", true)
		require.NoError(t, err)
		_, err = database.Vote(ctx, "RMHOST", "p2", "b", true)
		require.NoError(t, err)
		_, err = database.Vote(ctx, "RMHOST", "p3", "b", true)
		require.NoError(t, err)
		_, _, _, _, _, err = database.SetRoundReady(ctx, "RMHOST", "p1", 1, true)
		require.NoError(t, err)

		require.NoError(t, database.RemoveParticipant(ctx, "RMHOST", "hash1", "p2"))

		_, err = database.RoomState(ctx, "RMHOST", "p2")
		require.ErrorIs(t, err, ErrNotFound)

		state, err := database.RoomState(ctx, "RMHOST", "p1")
		require.NoError(t, err)
		assert.Len(t, state.Participants, 2)
		assert.Equal(t, "p1", state.Room.OwnerID)
		assert.Equal(t, 0, state.NextRound.Ready)
		assert.Nil(t, state.NextRound.RequestedBy)
		assert.False(t, state.Participants[0].ReadyForNextRound)
		assert.False(t, state.Participants[1].ReadyForNextRound)
	})

	t.Run("non host cannot remove participants", func(t *testing.T) {
		ctx := context.Background()
		database, err := Open(":memory:")
		require.NoError(t, err)
		defer database.Close() // nolint:errcheck

		item := plex.Item{RatingKey: "a", Library: "1", Type: "movie", Title: "Alpha"}
		require.NoError(t, database.SaveLibrary(ctx, plex.Library{Key: "1", Title: "Films"}, []plex.Item{item}))
		now := time.Now().UTC()
		require.NoError(t, database.CreateRoom(ctx, Room{Code: "RMNOPE", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, Participant{ID: "p1", Name: "Host"}, "hash1", []string{"a"}, []string{"a"}))
		require.NoError(t, database.JoinRoom(ctx, "RMNOPE", Participant{ID: "p2", Name: "Guest"}, "hash2"))

		err = database.RemoveParticipant(ctx, "RMNOPE", "hash2", "p1")
		require.ErrorIs(t, err, ErrForbidden)

		state, err := database.RoomState(ctx, "RMNOPE", "p1")
		require.NoError(t, err)
		assert.Len(t, state.Participants, 2)
	})
}
