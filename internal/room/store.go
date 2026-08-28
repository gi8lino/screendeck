package room

import (
	"context"

	"github.com/gi8lino/screendeck/internal/media"
)

// Store defines the persistence operations required by the room service.
type Store interface {
	CreateRoom(context.Context, Room, Participant, string, []string, []string, MembershipCredential) error
	JoinRoom(context.Context, string, Participant, string, MembershipCredential) error
	ParticipantByToken(context.Context, string, string) (Participant, error)
	RoomGenres(context.Context, string) ([]string, error)
	RoomState(context.Context, string, string) (State, error)
	SetRoomLocked(context.Context, string, string, bool) error
	Vote(context.Context, string, string, string, bool) (bool, error)
	RemoveParticipant(context.Context, string, string, string) error
	LeaveRoom(context.Context, string, string) error
	AddMoreTitles(context.Context, string, string, int) (int, int, error)
	SetRoundReady(context.Context, string, string, int, bool) (RoundResult, error)
	SaveLibrary(context.Context, media.Library, []media.Item) error
	ItemPoster(context.Context, string) (string, error)
	RoomMemberships(context.Context, string) ([]Membership, error)
	RoomMembershipSession(context.Context, string, string) (MembershipSession, error)
}
