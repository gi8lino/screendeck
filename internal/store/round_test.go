package store

import (
	"testing"
	"time"

	"github.com/gi8lino/screendeck/internal/media"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoundReadinessNarrowsBeforeDeckCompletion(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	database, err := Open(":memory:")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	items := []media.Item{
		{ID: "a", LibraryKey: "1", Type: "movie", Title: "Alpha"},
		{ID: "b", LibraryKey: "1", Type: "movie", Title: "Beta"},
		{ID: "c", LibraryKey: "1", Type: "movie", Title: "Gamma"},
	}
	require.NoError(t, database.SaveLibrary(ctx, media.Library{Key: "1", Title: "Films"}, items))

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
	assert.Equal(t, "a", state.Matches[0].ID)

	_, _, _, _, _, err = database.SetRoundReady(ctx, "ROUND1", "p1", 2, true)
	require.Error(t, err)
}

func TestAddMoreTitlesUsesUnusedPool(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	database, err := Open(":memory:")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	items := []media.Item{
		{ID: "a", LibraryKey: "1", Type: "movie", Title: "Alpha"},
		{ID: "b", LibraryKey: "1", Type: "movie", Title: "Beta"},
		{ID: "c", LibraryKey: "1", Type: "movie", Title: "Gamma"},
	}
	require.NoError(t, database.SaveLibrary(ctx, media.Library{Key: "1", Title: "Films"}, items))
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

func TestNextRoundRequestCancelsWhenMatchesDropBelowTwo(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	database, err := Open(":memory:")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	items := []media.Item{
		{ID: "a", LibraryKey: "1", Type: "movie", Title: "Alpha"},
		{ID: "b", LibraryKey: "1", Type: "movie", Title: "Beta"},
	}
	require.NoError(t, database.SaveLibrary(ctx, media.Library{Key: "1", Title: "Films"}, items))
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
	ctx := t.Context()
	database, err := Open(":memory:")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	items := []media.Item{
		{ID: "a", LibraryKey: "1", Type: "movie", Title: "Alpha"},
		{ID: "b", LibraryKey: "1", Type: "movie", Title: "Beta"},
	}
	require.NoError(t, database.SaveLibrary(ctx, media.Library{Key: "1", Title: "Films"}, items))
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

func TestConcurrentReadinessAdvancesExactlyOneRound(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	database := seedReadyConcurrencyRoom(t, "RACE02")
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

func TestConcurrentDuplicateReadinessIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	database := seedReadyConcurrencyRoom(t, "RACE03")
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

func TestConcurrentFinalReadyRequestIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	database := seedReadyConcurrencyRoom(t, "RACE04")
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

func TestReadyParticipantDepartureCancelsConsensus(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	database := seedReadyConcurrencyRoom(t, "RACE05")
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
func seedReadyConcurrencyRoom(t *testing.T, code string) *Store {
	t.Helper()
	ctx := t.Context()
	database, err := Open(":memory:")
	require.NoError(t, err)
	items := []media.Item{
		{ID: "a", LibraryKey: "1", Type: "movie", Title: "Alpha"},
		{ID: "b", LibraryKey: "1", Type: "movie", Title: "Beta"},
	}
	require.NoError(t, database.SaveLibrary(ctx, media.Library{Key: "1", Title: "Films"}, items))
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

func TestStaleRoundReadinessRequest(t *testing.T) {
	t.Parallel()

	t.Run("older round", func(t *testing.T) {
		t.Parallel()
		assert.True(t, staleRoundReadinessRequest(3, 2))
	})

	t.Run("current round", func(t *testing.T) {
		t.Parallel()
		assert.False(t, staleRoundReadinessRequest(2, 2))
	})

	t.Run("future round", func(t *testing.T) {
		t.Parallel()
		assert.False(t, staleRoundReadinessRequest(2, 3))
	})

	t.Run("unspecified round", func(t *testing.T) {
		t.Parallel()
		assert.False(t, staleRoundReadinessRequest(2, 0))
	})
}

func TestFutureRoundReadinessRequest(t *testing.T) {
	t.Parallel()

	t.Run("newer round", func(t *testing.T) {
		t.Parallel()
		assert.True(t, futureRoundReadinessRequest(2, 3))
	})

	t.Run("current round", func(t *testing.T) {
		t.Parallel()
		assert.False(t, futureRoundReadinessRequest(2, 2))
	})

	t.Run("older round", func(t *testing.T) {
		t.Parallel()
		assert.False(t, futureRoundReadinessRequest(3, 2))
	})

	t.Run("unspecified round", func(t *testing.T) {
		t.Parallel()
		assert.False(t, futureRoundReadinessRequest(2, 0))
	})
}

func TestRoomPhase(t *testing.T) {
	t.Parallel()

	t.Run("next round requested", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, RoomPhaseNextRoundRequested, roomPhase(1, 0, 1))
	})

	t.Run("single winner", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, RoomPhaseFinished, roomPhase(0, 0, 1))
	})

	t.Run("round complete without matches", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, RoomPhaseRoundComplete, roomPhase(0, 0, 0))
	})

	t.Run("round complete with multiple matches", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, RoomPhaseRoundComplete, roomPhase(0, 0, 2))
	})

	t.Run("swiping", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, RoomPhaseSwiping, roomPhase(0, 1, 1))
	})
}
