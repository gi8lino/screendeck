package media

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testProviderStore struct {
	provider ProviderID
	err      error
}

func (s *testProviderStore) LoadMediaProvider(context.Context) (ProviderID, error) {
	if s.err != nil {
		return "", s.err
	}
	if s.provider == "" {
		return "", ErrProviderNotFound
	}
	return s.provider, nil
}

func (s *testProviderStore) SaveMediaProvider(_ context.Context, provider ProviderID) error {
	s.provider = provider
	s.err = nil
	return nil
}

type testProvider struct {
	id         ProviderID
	configured bool
	name       string
}

func (p *testProvider) ID() ProviderID             { return p.id }
func (p *testProvider) Configured() (bool, string) { return p.configured, p.name }
func (p *testProvider) Libraries(context.Context) ([]Library, error) {
	return []Library{{Key: "1", Title: "Movies", Type: "movie"}}, nil
}
func (p *testProvider) Items(context.Context, Library) ([]Item, error)         { return nil, nil }
func (p *testProvider) Poster(context.Context, string) (*http.Response, error) { return nil, nil }

func TestManager(t *testing.T) {
	t.Run("restores active provider", func(t *testing.T) {
		store := &testProviderStore{provider: ProviderPlex}
		manager, err := NewManager(
			t.Context(),
			store,
			&testProvider{id: ProviderPlex, configured: true, name: "Plex"},
		)
		require.NoError(t, err)
		assert.Equal(t, Status{Configured: true, Provider: ProviderPlex, ServerName: "Plex"}, manager.Status())
	})

	t.Run("recovers single configured provider", func(t *testing.T) {
		store := &testProviderStore{}
		manager, err := NewManager(
			t.Context(),
			store,
			&testProvider{id: ProviderPlex},
			&testProvider{id: ProviderJellyfin, configured: true, name: "Jellyfin"},
		)
		require.NoError(t, err)
		assert.Equal(t, ProviderJellyfin, store.provider)
		assert.True(t, manager.Status().Configured)
	})

	t.Run("rejects provider switch", func(t *testing.T) {
		store := &testProviderStore{provider: ProviderPlex}
		manager, err := NewManager(
			t.Context(),
			store,
			&testProvider{id: ProviderPlex, configured: true},
			&testProvider{id: ProviderJellyfin, configured: true},
		)
		require.NoError(t, err)
		err = manager.SetActive(t.Context(), ProviderJellyfin)
		assert.ErrorIs(t, err, ErrProviderConflict)
	})

	t.Run("requires active provider for catalog access", func(t *testing.T) {
		manager, err := NewManager(t.Context(), &testProviderStore{})
		require.NoError(t, err)
		_, err = manager.Libraries(t.Context())
		assert.ErrorIs(t, err, ErrNotConfigured)
	})

	t.Run("propagates restore error", func(t *testing.T) {
		manager, err := NewManager(t.Context(), &testProviderStore{err: errors.New("boom")})
		assert.Nil(t, manager)
		assert.Error(t, err)
	})

	t.Run("rejects duplicate providers", func(t *testing.T) {
		manager, err := NewManager(
			t.Context(),
			&testProviderStore{},
			&testProvider{id: ProviderPlex},
			&testProvider{id: ProviderPlex},
		)
		assert.Nil(t, manager)
		assert.ErrorIs(t, err, ErrDuplicateProvider)
	})
}
