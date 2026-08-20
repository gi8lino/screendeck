package store

import (
	"context"
	"testing"
	"time"

	"github.com/gi8lino/screendeck/internal/plex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRoomMemberships verifies browser identities list only their active room memberships.
func TestRoomMemberships(t *testing.T) {
	ctx := context.Background()
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

// TestRoomMembershipSession verifies persisted room sessions can be restored without storing plaintext tokens.
func TestRoomMembershipSession(t *testing.T) {
	ctx := context.Background()
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

// TestSaveRoomMembership verifies existing sessions can be claimed and conflicting identities are rejected.
func TestSaveRoomMembership(t *testing.T) {
	ctx := context.Background()
	database := newMembershipTestStore(t, ctx)
	defer database.Close() // nolint:errcheck

	now := time.Now().UTC()
	require.NoError(t, database.CreateRoom(
		ctx,
		Room{Code: "MEM003", CreatedAt: now, ExpiresAt: now.Add(time.Hour)},
		Participant{ID: "host", Name: "Host"},
		"host-hash",
		[]string{"item"},
		[]string{"item"},
	))
	require.NoError(t, database.SaveRoomMembership(
		ctx,
		"MEM003",
		"host",
		RoomMembershipCredential{IdentityHash: "identity-host", SessionToken: "host-token"},
	))

	session, err := database.RoomMembershipSession(ctx, "identity-host", "MEM003")
	require.NoError(t, err)
	assert.Equal(t, "host-token", session.Token)

	require.NoError(t, database.JoinRoom(ctx, "MEM003", Participant{ID: "guest", Name: "Guest"}, "guest-hash"))
	err = database.SaveRoomMembership(
		ctx,
		"MEM003",
		"guest",
		RoomMembershipCredential{IdentityHash: "identity-host", SessionToken: "guest-token"},
	)
	require.ErrorIs(t, err, ErrMembershipConflict)

	require.NoError(t, database.SaveRoomMembership(
		ctx,
		"MEM003",
		"host",
		RoomMembershipCredential{IdentityHash: "identity-other", SessionToken: "host-token"},
	))
	_, err = database.RoomMembershipSession(ctx, "identity-host", "MEM003")
	require.ErrorIs(t, err, ErrNotFound)
	transferred, err := database.RoomMembershipSession(ctx, "identity-other", "MEM003")
	require.NoError(t, err)
	assert.Equal(t, "host-token", transferred.Token)
}

// newMembershipTestStore creates a store with one media item available for room tests.
func newMembershipTestStore(t *testing.T, ctx context.Context) *Store {
	t.Helper()
	database, err := Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, database.SaveLibrary(
		ctx,
		plex.Library{Key: "1", Title: "Films", Type: "movie"},
		[]plex.Item{{RatingKey: "item", Library: "1", Type: "movie", Title: "Arrival"}},
	))
	return database
}
