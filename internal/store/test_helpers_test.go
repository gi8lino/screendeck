package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
)

// testRoomMembership returns deterministic valid browser membership credentials.
func testRoomMembership(seed string) RoomMembershipCredential {
	digest := sha256.Sum256([]byte(seed))
	return RoomMembershipCredential{
		IdentityHash: hex.EncodeToString(digest[:]),
		SessionToken: "session-" + seed,
	}
}

// createTestRoom creates a room with deterministic browser membership credentials.
func createTestRoom(
	database *Store,
	ctx context.Context,
	room Room,
	participant Participant,
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
	participant Participant,
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
