package plex

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPreferredConnection verifies local direct connections outrank remote and relay connections.
func TestPreferredConnection(t *testing.T) {
	connections := []connection{
		{URI: "https://relay.example.test", Relay: true},
		{URI: "https://remote.example.test"},
		{URI: "http://192.0.2.10:32400", Local: true},
	}

	selected, ok := preferredConnection(connections)
	require.True(t, ok)
	assert.Equal(t, "http://192.0.2.10:32400", selected.URI)
	assert.True(t, selected.Local)
	assert.False(t, selected.Relay)

	selected, ok = preferredConnection([]connection{
		{URI: "://invalid", Local: true},
		{URI: "https://remote.example.test"},
	})
	require.True(t, ok)
	assert.Equal(t, "https://remote.example.test", selected.URI)
}
