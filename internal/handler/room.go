package handler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

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

// createRoomRequest describes the JSON payload accepted when creating a room.
type createRoomRequest struct {
	Name             string                `json:"name"`
	LibraryKeys      []string              `json:"libraryKeys"`
	Filters          room.Filters          `json:"filters"`
	Genres           []string              `json:"genres"`
	GenreMode        room.GenreMode        `json:"genreMode"`
	RoundSize        int                   `json:"roundSize"`
	LifetimeHours    int                   `json:"lifetimeHours"`
	SamplingStrategy room.SamplingStrategy `json:"samplingStrategy"`
}

// Valid returns every field-level room creation problem.
func (input createRoomRequest) Valid(context.Context) map[string]string {
	problems := make(map[string]string)

	if strings.TrimSpace(input.Name) == "" {
		problems["name"] = "Enter your name."
	}

	if len(input.LibraryKeys) == 0 {
		problems["libraryKeys"] = "Select at least one library."
	}

	if input.Filters.YearFrom < 0 {
		problems["filters.yearFrom"] = "The year must be zero or later."
	}

	if input.Filters.YearTo < 0 {
		problems["filters.yearTo"] = "The year must be zero or later."
	}

	if room.ReversedYearRange(input.Filters) {
		problems["filters.yearTo"] = "The final year must not be earlier than the starting year."
	}

	if input.Filters.MaxDurationMinutes < 0 {
		problems["filters.maxDurationMinutes"] = "The duration must be zero or greater."
	}

	if !room.ValidRoundSize(input.RoundSize) {
		problems["roundSize"] = "Choose a first-round size between 0 and 50,000."
	}

	if !room.ValidRoomLifetimeHours(input.LifetimeHours) {
		problems["lifetimeHours"] = "Choose a room lifetime between 6 hours and 7 days."
	}

	if !room.ValidGenreMode(input.GenreMode) {
		problems["genreMode"] = "Choose whether genres should match any or all selections."
	}

	if input.SamplingStrategy != "" && !room.ValidSamplingStrategy(input.SamplingStrategy) {
		problems["samplingStrategy"] = "Choose a valid first-round selection strategy."
	}

	return problems
}

// joinRoomRequest describes the JSON payload accepted when joining a room.
type joinRoomRequest struct {
	Code      string         `json:"code"`
	Name      string         `json:"name"`
	Genres    []string       `json:"genres"`
	GenreMode room.GenreMode `json:"genreMode"`
}

// Valid returns every field-level room joining problem.
func (input joinRoomRequest) Valid(context.Context) map[string]string {
	problems := make(map[string]string)
	if !validRoomCode(input.Code) {
		problems["code"] = "Enter a valid six-character room code."
	}
	if strings.TrimSpace(input.Name) == "" {
		problems["name"] = "Enter your name."
	}
	if !room.ValidGenreMode(input.GenreMode) {
		problems["genreMode"] = "Choose whether genres should match any or all selections."
	}
	return problems
}

// validRoomCode reports whether code has the expected length and alphabet.
func validRoomCode(code string) bool {
	code = strings.ToUpper(strings.TrimSpace(code))
	if len(code) != 6 {
		return false
	}
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	for _, character := range code {
		if !strings.ContainsRune(alphabet, character) {
			return false
		}
	}
	return true
}

// participantToken extracts a participant token from a request.
func participantToken(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("X-Participant-Token"))
}

// CreateRoom returns the room creation handler.
func CreateRoom(rooms roomCreator, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		input, err := decodeValid[createRoomRequest](r)
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		identityToken, err := ensureBrowserIdentity(w, r)
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		session, err := rooms.CreateForIdentity(
			r.Context(),
			input.Name,
			input.LibraryKeys,
			input.Filters,
			input.Genres,
			input.GenreMode,
			input.SamplingStrategy,
			input.RoundSize,
			input.LifetimeHours,
			identityToken,
		)
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		respond(logger, w, http.StatusCreated, session)
	}
}

// roomSettingsRequest describes host-controlled room admission settings.
type roomSettingsRequest struct {
	Locked *bool `json:"locked"`
}

// Valid validates a room settings update.
func (input roomSettingsRequest) Valid(context.Context) map[string]string {
	if input.Locked == nil {
		return map[string]string{"locked": "Choose whether the room accepts new participants."}
	}
	return nil
}

// UpdateRoomSettings returns the handler that changes host-controlled room settings.
func UpdateRoomSettings(rooms roomSettingsUpdater, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		input, err := decodeValid[roomSettingsRequest](r)
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		if err := rooms.SetRoomLocked(
			r.Context(),
			r.PathValue("code"),
			participantToken(r),
			*input.Locked,
		); err != nil {
			fail(logger, r, w, err)
			return
		}
		respond(logger, w, http.StatusOK, map[string]bool{"locked": *input.Locked})
	}
}

// JoinRoom returns the room joining handler.
func JoinRoom(rooms roomJoiner, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		input, err := decodeValid[joinRoomRequest](r)
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		identityToken, err := ensureBrowserIdentity(w, r)
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		session, err := rooms.JoinForIdentity(
			r.Context(),
			input.Code,
			input.Name,
			input.Genres,
			input.GenreMode,
			identityToken,
		)
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		respond(logger, w, http.StatusCreated, session)
	}
}

// RoomGenres returns the personal genre choices available in a room.
func RoomGenres(rooms roomGenreReader, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		genres, err := rooms.Genres(r.Context(), r.PathValue("code"))
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		respond(logger, w, http.StatusOK, map[string][]string{"genres": genres})
	}
}

// RoomState returns the current room state handler.
func RoomState(rooms roomStateReader, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state, err := rooms.State(r.Context(), r.PathValue("code"), participantToken(r))
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		respond(logger, w, http.StatusOK, state)
	}
}

// Vote returns the media voting handler.
func Vote(rooms roomVoter, logger *slog.Logger) http.HandlerFunc {
	// request describes the JSON payload accepted by this handler.
	type request struct {
		// ItemID identifies the canonical media item being voted on.
		ItemID string `json:"itemId"`
		// Liked records whether the participant accepted the item.
		Liked bool `json:"liked"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		input, err := decode[request](r)
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		if input.ItemID == "" {
			fail(logger, r, w, errors.New("itemId is required"))
			return
		}
		matched, err := rooms.Vote(r.Context(), r.PathValue("code"), participantToken(r), input.ItemID, input.Liked)
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		respond(logger, w, http.StatusOK, map[string]bool{"matched": matched})
	}
}

// AddMoreTitles returns the handler that expands the first round from its unused pool.
func AddMoreTitles(rooms roomExpander, logger *slog.Logger) http.HandlerFunc {
	// request describes the JSON payload accepted by this handler.
	type request struct {
		// Count is the requested number of additional titles.
		Count int `json:"count"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		input, err := decode[request](r)
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		result, err := rooms.AddMoreTitles(r.Context(), r.PathValue("code"), participantToken(r), input.Count)
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		respond(logger, w, http.StatusOK, result)
	}
}

// NextRoundReady returns the handler that records agreement to narrow the deck to current matches.
func NextRoundReady(rooms roundReadinessUpdater, logger *slog.Logger) http.HandlerFunc {
	// request describes the JSON payload accepted by this handler.
	type request struct {
		// Round identifies the room round the readiness update applies to.
		Round int `json:"round"`
		// Ready records whether the participant is ready to advance.
		Ready bool `json:"ready"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		input, err := decode[request](r)
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		result, err := rooms.SetNextRoundReady(
			r.Context(),
			r.PathValue("code"),
			participantToken(r),
			input.Round,
			input.Ready,
		)
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		respond(logger, w, http.StatusOK, result)
	}
}

// LeaveRoom returns the room departure handler.
func LeaveRoom(rooms roomLeaver, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := rooms.Leave(r.Context(), r.PathValue("code"), participantToken(r)); err != nil {
			fail(logger, r, w, err)
			return
		}
		respond(logger, w, http.StatusOK, map[string]string{"status": "left"})
	}
}

// RemoveParticipant returns the handler that lets a room host remove another participant.
func RemoveParticipant(rooms participantRemover, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := rooms.RemoveParticipant(
			r.Context(),
			r.PathValue("code"),
			participantToken(r),
			r.PathValue("participantID"),
		); err != nil {
			fail(logger, r, w, err)
			return
		}
		respond(logger, w, http.StatusOK, map[string]string{"status": "removed"})
	}
}

// Events returns the room server-sent events handler.
func Events(rooms roomEventSource, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code, token := strings.ToUpper(r.PathValue("code")), r.URL.Query().Get("token")
		if _, err := rooms.State(r.Context(), code, token); err != nil {
			fail(logger, r, w, err)
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		if err := disableWriteDeadline(w); err != nil {
			fail(logger, r, w, err)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		events, unsubscribe := rooms.Subscribe(code)
		defer unsubscribe()
		if _, err := io.WriteString(w, "event: update\ndata: connected\n\n"); err != nil {
			return
		}
		flusher.Flush()
		heartbeat := time.NewTicker(20 * time.Second)
		defer heartbeat.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-events:
				if _, err := io.WriteString(w, "event: update\ndata: changed\n\n"); err != nil {
					return
				}
				flusher.Flush()
			case <-heartbeat.C:
				if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}
}

// disableWriteDeadline allows a server-sent event response to remain open indefinitely.
func disableWriteDeadline(w http.ResponseWriter) error {
	err := http.NewResponseController(w).SetWriteDeadline(time.Time{})
	if errors.Is(err, http.ErrNotSupported) {
		return nil
	}
	return err
}
