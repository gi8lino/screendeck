package handler

import (
	"testing"

	"github.com/gi8lino/screendeck/internal/room"
	"github.com/stretchr/testify/assert"
)

// TestCreateRoomRequestValidation verifies every invalid room creation field is reported.
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

// TestJoinRoomRequestValidation verifies every invalid room joining field is reported.
func TestJoinRoomRequestValidation(t *testing.T) {
	input := joinRoomRequest{Code: "IO10", GenreMode: "invalid"}

	problems := input.Valid(t.Context())

	assert.Contains(t, problems, "code")
	assert.Contains(t, problems, "name")
	assert.Contains(t, problems, "genreMode")
}
