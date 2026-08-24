package store

import (
	"context"
	"testing"
	"time"

	"github.com/gi8lino/screendeck/internal/media"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoomMemberships(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	database := newMembershipTestStore(t, ctx)
	defer database.Close() // nolint:errcheck

	now := time.Now().UTC()
	require.NoError(t, database.CreateRoom(
		ctx,
		Room{Code: "MEM001", CreatedAt: now, ExpiresAt: now.Add(time.Hour)},
		Participant{ID: "host", Name: "Host"},
		"host-hash",
		[]string{"item"},
		[]string{"item"},
		RoomMembershipCredential{IdentityHash: "identity-host", SessionToken: "host-token"},
	))
	require.NoError(t, database.JoinRoom(
		ctx,
		"MEM001",
		Participant{ID: "guest", Name: "Guest"},
		"guest-hash",
		RoomMembershipCredential{IdentityHash: "identity-guest", SessionToken: "guest-token"},
	))

	hostRooms, err := database.RoomMemberships(ctx, "identity-host")
	require.NoError(t, err)
	require.Len(t, hostRooms, 1)
	assert.Equal(t, "MEM001", hostRooms[0].Code)
	assert.Equal(t, "Host", hostRooms[0].Name)
	assert.True(t, hostRooms[0].IsHost)
	assert.Equal(t, 2, hostRooms[0].ParticipantCount)

	guestRooms, err := database.RoomMemberships(ctx, "identity-guest")
	require.NoError(t, err)
	require.Len(t, guestRooms, 1)
	assert.False(t, guestRooms[0].IsHost)

	require.NoError(t, database.RemoveParticipant(ctx, "MEM001", "host-hash", "guest"))
	guestRooms, err = database.RoomMemberships(ctx, "identity-guest")
	require.NoError(t, err)
	assert.Empty(t, guestRooms)

	hostRooms, err = database.RoomMemberships(ctx, "identity-host")
	require.NoError(t, err)
	require.Len(t, hostRooms, 1)
	assert.Equal(t, 1, hostRooms[0].ParticipantCount)
}

func TestRoomMembershipSession(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	database := newMembershipTestStore(t, ctx)
	defer database.Close() // nolint:errcheck

	now := time.Now().UTC()
	require.NoError(t, database.CreateRoom(
		ctx,
		Room{Code: "MEM002", CreatedAt: now, ExpiresAt: now.Add(time.Hour)},
		Participant{ID: "host", Name: "Host"},
		"host-hash",
		[]string{"item"},
		[]string{"item"},
		RoomMembershipCredential{IdentityHash: "identity-host", SessionToken: "session-secret"},
	))

	session, err := database.RoomMembershipSession(ctx, "identity-host", "MEM002")
	require.NoError(t, err)
	assert.Equal(t, "MEM002", session.Code)
	assert.Equal(t, "session-secret", session.Token)

	var storedToken []byte
	require.NoError(t, database.db.QueryRowContext(
		ctx,
		"SELECT session_token FROM room_memberships WHERE identity_hash = ? AND room_code = ?",
		"identity-host",
		"MEM002",
	).Scan(&storedToken))
	assert.NotEqual(t, []byte("session-secret"), storedToken)

	_, err = database.RoomMembershipSession(ctx, "identity-other", "MEM002")
	require.ErrorIs(t, err, ErrNotFound)
}

// newMembershipTestStore creates a store with one media item available for room tests.
func newMembershipTestStore(t *testing.T, ctx context.Context) *Store {
	t.Helper()
	database, err := Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, database.SaveLibrary(
		ctx,
		media.Library{Key: "1", Title: "Films", Type: "movie"},
		[]media.Item{{ID: "item", LibraryKey: "1", Type: "movie", Title: "Arrival"}},
	))
	return database
}

// TestValidateIdentityHash verifies browser identity hash validation.
func TestValidateIdentityHash(t *testing.T) {
	t.Parallel()

	t.Run("valid", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, validateIdentityHash("identity-hash"))
	})

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		require.Error(t, validateIdentityHash(""))
	})

	t.Run("reserved invalid value", func(t *testing.T) {
		t.Parallel()
		require.Error(t, validateIdentityHash("invalid"))
	})
}

func TestValidateRoomMembershipCredential(t *testing.T) {
	t.Parallel()

	t.Run("valid", func(t *testing.T) {
		t.Parallel()

		require.NoError(t, validateRoomMembershipCredential(RoomMembershipCredential{
			IdentityHash: "identity-hash",
			SessionToken: "participant-token",
		}))
	})

	t.Run("missing identity", func(t *testing.T) {
		t.Parallel()

		err := validateRoomMembershipCredential(RoomMembershipCredential{SessionToken: "participant-token"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "browser identity")
	})

	t.Run("missing session token", func(t *testing.T) {
		t.Parallel()

		err := validateRoomMembershipCredential(RoomMembershipCredential{IdentityHash: "identity-hash"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "participant session token")
	})
}
