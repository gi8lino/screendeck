package handler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestJellyfinConnectRequestValidation verifies invalid Jellyfin connection fields are reported.
func TestJellyfinConnectRequestValidation(t *testing.T) {
	input := jellyfinConnectRequest{ServerURL: "jellyfin.local"}

	problems := input.Valid(t.Context())

	assert.Contains(t, problems, "serverUrl")
	assert.Contains(t, problems, "username")
}
