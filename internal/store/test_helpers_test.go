package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	roomdomain "github.com/gi8lino/screendeck/internal/room"
)

// testRoomMembership returns deterministic valid browser membership credentials.
func testRoomMembership(seed string) roomdomain.MembershipCredential {
	digest := sha256.Sum256([]byte(seed))
	return roomdomain.MembershipCredential{
		IdentityHash: hex.EncodeToString(digest[:]),
		SessionToken: "session-" + seed,
	}
}

// createTestRoom creates a room with deterministic browser membership credentials.
func createTestRoom(
	database *Store,
	ctx context.Context,
	room roomdomain.Room,
	participant roomdomain.Participant,
	tokenHash string,
	itemIDs []string,
	poolIDs []string,
) error {
	return database.CreateRoom(
		ctx,
		room,
		participant,
		tokenHash,
		itemIDs,
		poolIDs,
		testRoomMembership(tokenHash),
	)
}

// joinTestRoom joins a participant with deterministic browser membership credentials.
func joinTestRoom(
	database *Store,
	ctx context.Context,
	code string,
	participant roomdomain.Participant,
	tokenHash string,
) error {
	return database.JoinRoom(
		ctx,
		code,
		participant,
		tokenHash,
		testRoomMembership(tokenHash),
	)
}
