package app

import (
	"bytes"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRun verifies top-level command handling that completes before application startup.
func TestRun(t *testing.T) {
	t.Parallel()

	appFS := fstest.MapFS{
		"index.html": {Data: []byte("ScreenDeck")},
	}

	t.Run("help exits successfully", func(t *testing.T) {
		t.Parallel()

		var stdout bytes.Buffer
		var stderr bytes.Buffer

		err := Run(
			t.Context(),
			appFS,
			"test-version",
			"test-commit",
			[]string{"--help"},
			&stdout,
			&stderr,
		)

		require.NoError(t, err)
		assert.Contains(t, strings.ToLower(stdout.String()), "usage")
		assert.Empty(t, stderr.String())
	})

	t.Run("invalid configuration fails before startup", func(t *testing.T) {
		t.Parallel()

		var stdout bytes.Buffer
		var stderr bytes.Buffer

		err := Run(
			t.Context(),
			appFS,
			"test-version",
			"test-commit",
			[]string{"--base-url", "/relative"},
			&stdout,
			&stderr,
		)

		require.Error(t, err)
		assert.Empty(t, stdout.String())
		assert.Contains(t, stderr.String(), "absolute HTTP or HTTPS URL")
	})
}
