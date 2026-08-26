package store

import roomdomain "github.com/gi8lino/screendeck/internal/room"

const (
	// posterLookaheadSize limits how many upcoming posters the browser preloads.
	posterLookaheadSize = 3
)

type roomRecord = roomdomain.Room
type roomPhase = roomdomain.Phase
type roomParticipant = roomdomain.Participant
type moreTitlesState = roomdomain.MoreTitlesState
type winnerState = roomdomain.WinnerState
type roomState = roomdomain.State
type roomProgress = roomdomain.Progress
type nextRoundState = roomdomain.NextRoundState
