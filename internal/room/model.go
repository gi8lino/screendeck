package room

import (
	"time"

	"github.com/gi8lino/screendeck/internal/media"
)

// Phase identifies the current lifecycle phase of a room.
type Phase string

const (
	// PhaseSwiping indicates that participants can still vote in the current round.
	PhaseSwiping Phase = "swiping"
	// PhaseNextRoundRequested indicates that at least one participant requested the next round.
	PhaseNextRoundRequested Phase = "next_round_requested"
	// PhaseRoundComplete indicates that no eligible votes remain and the round has no single winner.
	PhaseRoundComplete Phase = "round_complete"
	// PhaseFinished indicates that the room has converged on one winning item.
	PhaseFinished Phase = "finished"
)

// Room contains room metadata.
type Room struct {
	Code      string    `json:"code"`
	Round     int       `json:"round"`
	Phase     Phase     `json:"phase"`
	OwnerID   string    `json:"ownerId"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	Locked    bool      `json:"locked"`
}

// Participant contains public room participant state.
type Participant struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Genres            []string `json:"genres"`
	GenreMode         string   `json:"genreMode"`
	IsHost            bool     `json:"isHost"`
	ReadyForNextRound bool     `json:"readyForNextRound"`
}

// MoreTitlesState reports whether unused first-round titles can be added.
type MoreTitlesState struct {
	Available int  `json:"available"`
	CanAdd    bool `json:"canAdd"`
}

// WinnerState contains the final winning item and its supporters.
type WinnerState struct {
	Item    media.Item    `json:"item"`
	LikedBy []Participant `json:"likedBy"`
}

// State contains the participant-specific view of a room.
type State struct {
	Room            Room            `json:"room"`
	Me              Participant     `json:"me"`
	Participants    []Participant   `json:"participants"`
	Candidate       *media.Item     `json:"candidate,omitempty"`
	PosterLookahead []string        `json:"posterLookahead,omitempty"`
	Matches         []media.Item    `json:"matches"`
	Winner          *WinnerState    `json:"winner,omitempty"`
	Progress        Progress        `json:"progress"`
	NextRound       NextRoundState  `json:"nextRound"`
	RoundComplete   bool            `json:"roundComplete"`
	MoreTitles      MoreTitlesState `json:"moreTitles"`
}

// Progress reports swipe progress for the current participant and round.
type Progress struct {
	Voted       int `json:"voted"`
	Total       int `json:"total"`
	RoundTotal  int `json:"roundTotal"`
	FilteredOut int `json:"filteredOut"`
}

// NextRoundState reports group consensus for advancing to the next round.
type NextRoundState struct {
	Ready       int          `json:"ready"`
	Required    int          `json:"required"`
	Available   bool         `json:"available"`
	RequestedBy *Participant `json:"requestedBy,omitempty"`
}

// MembershipCredential links a room participant to a persistent browser identity.
type MembershipCredential struct {
	IdentityHash string
	SessionToken string
}

// Membership describes an active room associated with one browser identity.
type Membership struct {
	Code             string    `json:"code"`
	Round            int       `json:"round"`
	Phase            Phase     `json:"phase"`
	ParticipantID    string    `json:"participantId"`
	Name             string    `json:"name"`
	IsHost           bool      `json:"isHost"`
	ParticipantCount int       `json:"participantCount"`
	CreatedAt        time.Time `json:"createdAt"`
	ExpiresAt        time.Time `json:"expiresAt"`
}

// MembershipSession contains participant credentials persisted for a browser identity.
type MembershipSession struct {
	Code  string
	Token string
}
