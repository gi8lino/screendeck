package store

import (
	"testing"
	"time"

	"github.com/gi8lino/screendeck/internal/media"
	roomdomain "github.com/gi8lino/screendeck/internal/room"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnanimousMatchLifecycle(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	database, err := Open(":memory:", "")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	item := media.Item{ID: "42", LibraryKey: "1", Type: "movie", Title: "Arrival", Genres: []string{"Science Fiction"}}
	require.NoError(t, database.SaveLibrary(ctx, media.Library{Key: "1", Title: "Films"}, []media.Item{item}))

	now := time.Now().UTC()
	require.NoError(t, createTestRoom(database, ctx, roomdomain.Room{Code: "ABC123", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, roomdomain.Participant{ID: "p1", Name: "One"}, "hash1", []string{"42"}, []string{"42"}))
	require.NoError(t, joinTestRoom(database, ctx, "ABC123", roomdomain.Participant{ID: "p2", Name: "Two"}, "hash2"))

	matched, err := database.Vote(ctx, "ABC123", "p1", "42", true)
	require.NoError(t, err)
	assert.False(t, matched)

	matched, err = database.Vote(ctx, "ABC123", "p2", "42", true)
	require.NoError(t, err)
	assert.True(t, matched)

	state, err := database.RoomState(ctx, "ABC123", "p2")
	require.NoError(t, err)
	assert.Nil(t, state.Candidate)
	assert.Equal(t, roomdomain.PhaseFinished, state.Room.Phase)
	assert.Len(t, state.Matches, 1)
	require.NotNil(t, state.Winner)
	assert.Equal(t, "42", state.Winner.Item.ID)
	assert.Len(t, state.Winner.LikedBy, 2)
	assert.Equal(t, "One", state.Winner.LikedBy[0].Name)
	assert.Equal(t, "Two", state.Winner.LikedBy[1].Name)
	assert.True(t, state.Participants[0].IsHost)
	assert.False(t, state.Participants[1].IsHost)
	assert.Equal(t, 1, state.Progress.Voted)
}

func TestRoomStateIncludesPosterLookahead(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	database, err := Open(":memory:", "")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	items := []media.Item{
		{ID: "a", LibraryKey: "1", Type: "movie", Title: "Alpha"},
		{ID: "b", LibraryKey: "1", Type: "movie", Title: "Beta"},
		{ID: "c", LibraryKey: "1", Type: "movie", Title: "Gamma"},
		{ID: "d", LibraryKey: "1", Type: "movie", Title: "Delta"},
		{ID: "e", LibraryKey: "1", Type: "movie", Title: "Epsilon"},
	}
	require.NoError(t, database.SaveLibrary(ctx, media.Library{Key: "1", Title: "Films"}, items))
	now := time.Now().UTC()
	require.NoError(t, createTestRoom(database,
		ctx,
		roomdomain.Room{Code: "POSTER", CreatedAt: now, ExpiresAt: now.Add(time.Hour)},
		roomdomain.Participant{ID: "p1", Name: "One"},
		"hash1",
		[]string{"a", "b", "c", "d", "e"},
		[]string{"a", "b", "c", "d", "e"},
	))

	state, err := database.RoomState(ctx, "POSTER", "p1")
	require.NoError(t, err)
	require.NotNil(t, state.Candidate)
	assert.Equal(t, "a", state.Candidate.ID)
	assert.Equal(t, []string{"b", "c", "d"}, state.PosterLookahead)

	_, err = database.Vote(ctx, "POSTER", "p1", "a", false)
	require.NoError(t, err)
	state, err = database.RoomState(ctx, "POSTER", "p1")
	require.NoError(t, err)
	require.NotNil(t, state.Candidate)
	assert.Equal(t, "b", state.Candidate.ID)
	assert.Equal(t, []string{"c", "d", "e"}, state.PosterLookahead)
}

func TestLeavingParticipantCanCompleteMatch(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	database, err := Open(":memory:", "")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	item := media.Item{ID: "7", LibraryKey: "1", Type: "movie", Title: "Alien"}
	require.NoError(t, database.SaveLibrary(ctx, media.Library{Key: "1", Title: "Films"}, []media.Item{item}))

	now := time.Now().UTC()
	require.NoError(t, createTestRoom(database, ctx, roomdomain.Room{Code: "LEAVE1", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, roomdomain.Participant{ID: "p1", Name: "One"}, "hash1", []string{"7"}, []string{"7"}))
	require.NoError(t, joinTestRoom(database, ctx, "LEAVE1", roomdomain.Participant{ID: "p2", Name: "Two"}, "hash2"))
	require.NoError(t, joinTestRoom(database, ctx, "LEAVE1", roomdomain.Participant{ID: "p3", Name: "Three"}, "hash3"))

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

func TestHostOwnershipTransfersOnLeave(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	database, err := Open(":memory:", "")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	item := media.Item{ID: "a", LibraryKey: "1", Type: "movie", Title: "Alpha"}
	require.NoError(t, database.SaveLibrary(ctx, media.Library{Key: "1", Title: "Films"}, []media.Item{item}))
	now := time.Now().UTC()
	require.NoError(t, createTestRoom(database, ctx, roomdomain.Room{Code: "HOST01", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, roomdomain.Participant{ID: "p1", Name: "Host"}, "hash1", []string{"a"}, []string{"a"}))
	require.NoError(t, joinTestRoom(database, ctx, "HOST01", roomdomain.Participant{ID: "p2", Name: "Next"}, "hash2"))

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

// TestRoomLock verifies hosts can control admission without removing existing participants.
func TestRoomLock(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	database, err := Open(":memory:", "")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	item := media.Item{ID: "a", LibraryKey: "1", Type: "movie", Title: "Alpha"}
	require.NoError(t, database.SaveLibrary(ctx, media.Library{Key: "1", Title: "Films"}, []media.Item{item}))
	now := time.Now().UTC()
	require.NoError(t, createTestRoom(database,
		ctx,
		roomdomain.Room{Code: "LOCK01", CreatedAt: now, ExpiresAt: now.Add(time.Hour)},
		roomdomain.Participant{ID: "host", Name: "Host"},
		"host-hash",
		[]string{"a"},
		[]string{"a"},
	))

	require.NoError(t, database.SetRoomLocked(ctx, "LOCK01", "host-hash", true))
	state, err := database.RoomState(ctx, "LOCK01", "host")
	require.NoError(t, err)
	assert.True(t, state.Room.Locked)

	err = joinTestRoom(database, ctx, "LOCK01", roomdomain.Participant{ID: "guest", Name: "Guest"}, "guest-hash")
	require.ErrorIs(t, err, roomdomain.ErrLocked)

	require.NoError(t, database.SetRoomLocked(ctx, "LOCK01", "host-hash", false))
	require.NoError(t, joinTestRoom(database, ctx, "LOCK01", roomdomain.Participant{ID: "guest", Name: "Guest"}, "guest-hash"))
	err = database.SetRoomLocked(ctx, "LOCK01", "guest-hash", true)
	require.ErrorIs(t, err, roomdomain.ErrForbidden)
}

func TestConcurrentFinalVotesCreateOneMatch(t *testing.T) {
	ctx := t.Context()
	database, err := Open(":memory:", "")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	item := media.Item{ID: "a", LibraryKey: "1", Type: "movie", Title: "Alpha"}
	require.NoError(t, database.SaveLibrary(ctx, media.Library{Key: "1", Title: "Films"}, []media.Item{item}))
	now := time.Now().UTC()
	require.NoError(t, createTestRoom(database, ctx, roomdomain.Room{Code: "RACE01", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, roomdomain.Participant{ID: "p1", Name: "One"}, "hash1", []string{"a"}, []string{"a"}))
	require.NoError(t, joinTestRoom(database, ctx, "RACE01", roomdomain.Participant{ID: "p2", Name: "Two"}, "hash2"))

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
	assert.Equal(t, roomdomain.PhaseFinished, state.Room.Phase)
}

func TestRemoveParticipant(t *testing.T) {
	t.Parallel()

	t.Run("host can remove another participant and readiness resets", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		database, err := Open(":memory:", "")
		require.NoError(t, err)
		defer database.Close() // nolint:errcheck

		items := []media.Item{
			{ID: "a", LibraryKey: "1", Type: "movie", Title: "Alpha"},
			{ID: "b", LibraryKey: "1", Type: "movie", Title: "Beta"},
		}
		require.NoError(t, database.SaveLibrary(ctx, media.Library{Key: "1", Title: "Films"}, items))
		now := time.Now().UTC()
		require.NoError(t, createTestRoom(database, ctx, roomdomain.Room{Code: "RMHOST", Round: 1, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, roomdomain.Participant{ID: "p1", Name: "Host"}, "hash1", []string{"a", "b"}, []string{"a", "b"}))
		require.NoError(t, joinTestRoom(database, ctx, "RMHOST", roomdomain.Participant{ID: "p2", Name: "Guest"}, "hash2"))
		require.NoError(t, joinTestRoom(database, ctx, "RMHOST", roomdomain.Participant{ID: "p3", Name: "Third"}, "hash3"))
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
		require.ErrorIs(t, err, roomdomain.ErrNotFound)

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
		t.Parallel()

		ctx := t.Context()
		database, err := Open(":memory:", "")
		require.NoError(t, err)
		defer database.Close() // nolint:errcheck

		item := media.Item{ID: "a", LibraryKey: "1", Type: "movie", Title: "Alpha"}
		require.NoError(t, database.SaveLibrary(ctx, media.Library{Key: "1", Title: "Films"}, []media.Item{item}))
		now := time.Now().UTC()
		require.NoError(t, createTestRoom(database, ctx, roomdomain.Room{Code: "RMNOPE", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, roomdomain.Participant{ID: "p1", Name: "Host"}, "hash1", []string{"a"}, []string{"a"}))
		require.NoError(t, joinTestRoom(database, ctx, "RMNOPE", roomdomain.Participant{ID: "p2", Name: "Guest"}, "hash2"))

		err = database.RemoveParticipant(ctx, "RMNOPE", "hash2", "p1")
		require.ErrorIs(t, err, roomdomain.ErrForbidden)

		state, err := database.RoomState(ctx, "RMNOPE", "p1")
		require.NoError(t, err)
		assert.Len(t, state.Participants, 2)
	})
}

func TestNormalizeRoomCreation(t *testing.T) {
	t.Parallel()

	t.Run("applies defaults", func(t *testing.T) {
		t.Parallel()

		room := roomdomain.Room{Code: "ABC123"}
		participant := roomdomain.Participant{ID: "participant-1"}

		normalizeRoomCreation(&room, &participant)

		assert.Equal(t, 1, room.Round)
		assert.Equal(t, roomdomain.PhaseSwiping, room.Phase)
		assert.Equal(t, "participant-1", room.OwnerID)
		require.NotNil(t, participant.Genres)
		assert.Empty(t, participant.Genres)
		assert.Equal(t, "any", participant.GenreMode)
	})

	t.Run("preserves explicit values", func(t *testing.T) {
		t.Parallel()

		room := roomdomain.Room{
			Code:    "DEF456",
			Round:   3,
			Phase:   roomdomain.PhaseFinished,
			OwnerID: "owner-2",
		}
		participant := roomdomain.Participant{
			ID:        "participant-2",
			Genres:    []string{"Drama"},
			GenreMode: "all",
		}

		normalizeRoomCreation(&room, &participant)

		assert.Equal(t, 3, room.Round)
		assert.Equal(t, roomdomain.PhaseFinished, room.Phase)
		assert.Equal(t, "owner-2", room.OwnerID)
		assert.Equal(t, []string{"Drama"}, participant.Genres)
		assert.Equal(t, "all", participant.GenreMode)
	})
}

func TestNormalizeParticipant(t *testing.T) {
	t.Parallel()

	t.Run("applies defaults", func(t *testing.T) {
		t.Parallel()

		participant := roomdomain.Participant{}

		normalizeParticipant(&participant)

		require.NotNil(t, participant.Genres)
		assert.Empty(t, participant.Genres)
		assert.Equal(t, "any", participant.GenreMode)
	})

	t.Run("preserves explicit preferences", func(t *testing.T) {
		participant := roomdomain.Participant{Genres: []string{"Drama"}, GenreMode: "all"}

		normalizeParticipant(&participant)

		assert.Equal(t, []string{"Drama"}, participant.Genres)
		assert.Equal(t, "all", participant.GenreMode)
	})
}

func TestEncodeParticipantGenres(t *testing.T) {
	t.Parallel()

	encoded, err := encodeParticipantGenres([]string{"Drama", "Science Fiction"})
	require.NoError(t, err)
	assert.Equal(t, `["Drama","Science Fiction"]`, encoded)
}
