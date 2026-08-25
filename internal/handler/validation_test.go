package handler

import (
	"testing"

	"github.com/gi8lino/screendeck/internal/plex"
	"github.com/gi8lino/screendeck/internal/room"
	"github.com/stretchr/testify/assert"
)

func TestCreateRoomRequestValidation(t *testing.T) {
	input := createRoomRequest{
		Filters: room.Filters{
			YearFrom:           2030,
			YearTo:             2020,
			MaxDurationMinutes: -1,
		},
		RoundSize:        50_001,
		LifetimeHours:    2,
		GenreMode:        "invalid",
		SamplingStrategy: "invalid",
	}

	problems := input.Valid(t.Context())

	assert.Contains(t, problems, "name")
	assert.Contains(t, problems, "libraryKeys")
	assert.Contains(t, problems, "filters.yearTo")
	assert.Contains(t, problems, "filters.maxDurationMinutes")
	assert.Contains(t, problems, "roundSize")
	assert.Contains(t, problems, "lifetimeHours")
	assert.Contains(t, problems, "genreMode")
	assert.Contains(t, problems, "samplingStrategy")
}

func TestJoinRoomRequestValidation(t *testing.T) {
	input := joinRoomRequest{Code: "IO10", GenreMode: "invalid"}

	problems := input.Valid(t.Context())

	assert.Contains(t, problems, "code")
	assert.Contains(t, problems, "name")
	assert.Contains(t, problems, "genreMode")
}

func TestJellyfinConnectRequestValidation(t *testing.T) {
	input := jellyfinConnectRequest{ServerURL: "jellyfin.local"}

	problems := input.Valid(t.Context())

	assert.Contains(t, problems, "serverUrl")
	assert.Contains(t, problems, "username")
}

func TestPlexAuthRequestValidation(t *testing.T) {
	input := plexAuthRequest{Method: plex.AuthMethod("invalid")}

	problems := input.Valid(t.Context())

	assert.Contains(t, problems, "method")
}

func TestSelectPlexServerRequestValidation(t *testing.T) {
	input := selectPlexServerRequest{ServerID: "  "}

	problems := input.Valid(t.Context())

	assert.Contains(t, problems, "serverId")
}
