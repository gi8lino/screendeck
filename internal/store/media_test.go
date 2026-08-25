package store

import (
	"testing"

	"github.com/gi8lino/screendeck/internal/media"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMediaProvider(t *testing.T) {
	database, err := Open(":memory:", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	_, err = database.LoadMediaProvider(t.Context())
	assert.ErrorIs(t, err, media.ErrProviderNotFound)

	require.NoError(t, database.SaveMediaProvider(t.Context(), media.ProviderJellyfin))
	provider, err := database.LoadMediaProvider(t.Context())
	require.NoError(t, err)
	assert.Equal(t, media.ProviderJellyfin, provider)
}
