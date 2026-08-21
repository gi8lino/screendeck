package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestOptionalKeyPath verifies only the first optional encryption-key path is used.
func TestOptionalKeyPath(t *testing.T) {
	t.Run("no path", func(t *testing.T) {
		assert.Empty(t, optionalKeyPath(nil))
	})

	t.Run("single path", func(t *testing.T) {
		assert.Equal(t, "/tmp/auth.key", optionalKeyPath([]string{"/tmp/auth.key"}))
	})

	t.Run("multiple paths", func(t *testing.T) {
		assert.Equal(t, "/tmp/first.key", optionalKeyPath([]string{"/tmp/first.key", "/tmp/second.key"}))
	})
}
