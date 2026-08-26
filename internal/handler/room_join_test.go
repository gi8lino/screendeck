package handler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestJoinRoomRequestValidation verifies every invalid room joining field is reported.
func TestJoinRoomRequestValidation(t *testing.T) {
	input := joinRoomRequest{Code: "IO10", GenreMode: "invalid"}

	problems := input.Valid(t.Context())

	assert.Contains(t, problems, "code")
	assert.Contains(t, problems, "name")
	assert.Contains(t, problems, "genreMode")
}
