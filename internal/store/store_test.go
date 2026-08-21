package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOptionalKeyPath(t *testing.T) {
	t.Parallel()

	t.Run("no path", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, optionalKeyPath(nil))
	})

	t.Run("single path", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "/tmp/auth.key", optionalKeyPath([]string{"/tmp/auth.key"}))
	})

	t.Run("multiple paths", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "/tmp/first.key", optionalKeyPath([]string{"/tmp/first.key", "/tmp/second.key"}))
	})
}
