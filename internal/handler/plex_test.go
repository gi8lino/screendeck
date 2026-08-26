package handler

import (
	"testing"

	"github.com/gi8lino/screendeck/internal/plex"
	"github.com/stretchr/testify/assert"
)

// TestPlexAuthRequestValidation verifies an unsupported Plex authentication method is reported.
func TestPlexAuthRequestValidation(t *testing.T) {
	input := plexAuthRequest{Method: plex.AuthMethod("invalid")}

	problems := input.Valid(t.Context())

	assert.Contains(t, problems, "method")
}

// TestSelectPlexServerRequestValidation verifies an empty Plex server selection is reported.
func TestSelectPlexServerRequestValidation(t *testing.T) {
	input := selectPlexServerRequest{ServerID: "  "}

	problems := input.Valid(t.Context())

	assert.Contains(t, problems, "serverId")
}
