package handler

import (
	"context"

	"github.com/gi8lino/screendeck/internal/room"
)

// roomCreator creates rooms for persistent browser identities.
type roomCreator interface {
	CreateForIdentity(
		context.Context,
		string,
		[]string,
		room.Filters,
		[]string,
		room.GenreMode,
		room.SamplingStrategy,
		int,
		int,
		string,
	) (room.Session, error)
}

// roomSettingsUpdater changes host-controlled room settings.
type roomSettingsUpdater interface {
	SetRoomLocked(context.Context, string, string, bool) error
}

// roomJoiner adds a browser identity to a room.
type roomJoiner interface {
	JoinForIdentity(
		context.Context,
		string,
		string,
		[]string,
		room.GenreMode,
		string,
	) (room.Session, error)
}

// roomGenreReader lists genres represented by a room deck.
type roomGenreReader interface {
	Genres(context.Context, string) ([]string, error)
}

// roomStateReader returns participant-specific room state.
type roomStateReader interface {
	State(context.Context, string, string) (room.State, error)
}

// roomVoter records participant votes.
type roomVoter interface {
	Vote(context.Context, string, string, string, bool) (bool, error)
}

// roomExpander adds unused titles to a room's first round.
type roomExpander interface {
	AddMoreTitles(context.Context, string, string, int) (room.MoreTitlesResult, error)
}

// roundReadinessUpdater records participant readiness for the next round.
type roundReadinessUpdater interface {
	SetNextRoundReady(context.Context, string, string, int, bool) (room.RoundResult, error)
}

// roomLeaver removes the authenticated participant from a room.
type roomLeaver interface {
	Leave(context.Context, string, string) error
}

// participantRemover removes another participant at the host's request.
type participantRemover interface {
	RemoveParticipant(context.Context, string, string, string) error
}

// roomEventSource authenticates event subscribers and publishes room changes.
type roomEventSource interface {
	roomStateReader
	Subscribe(string) (<-chan struct{}, func())
}
