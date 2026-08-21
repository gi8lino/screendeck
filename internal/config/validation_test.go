package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestParseRejectsNonPositiveDurations verifies room lifetime and cleanup cadence must be positive.
func TestParseRejectsNonPositiveDurations(t *testing.T) {
	_, err := Parse([]string{"--room-ttl", "0s"}, "test")
	require.Error(t, err)

	_, err = Parse([]string{"--room-cleanup-interval", "0s"}, "test")
	require.Error(t, err)
}
