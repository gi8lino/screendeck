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

	var authMethod string
	var encryptedPrivate, encryptedUser, encryptedServer []byte
	const encryptedAuthQuery = `
SELECT
  auth_method,
  private_key,
  user_token,
  server_token
FROM plex_auth
WHERE id = 1
`
	require.NoError(t, database.db.QueryRow(encryptedAuthQuery).Scan(&authMethod, &encryptedPrivate, &encryptedUser, &encryptedServer))
	assert.Equal(t, string(plex.AuthMethodJWT), authMethod)
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

// TestStandardPlexAuthenticationRoundTrip verifies standard authentication does not require a device key.
func TestStandardPlexAuthenticationRoundTrip(t *testing.T) {
	database, err := Open(":memory:")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	state := plex.AuthState{
		Method: plex.AuthMethodStandard, ClientID: "client", UserToken: "standard-user-token",
		ServerID: "server", ServerName: "Plex", ServerURL: "http://plex.test:32400", ServerToken: "standard-server-token",
	}
	require.NoError(t, database.SavePlexAuth(context.Background(), state))

	loaded, err := database.LoadPlexAuth(context.Background())
	require.NoError(t, err)
	assert.Equal(t, plex.AuthMethodStandard, loaded.Method)
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
