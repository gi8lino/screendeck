package media

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
)

// Manager selects one configured media provider and exposes it as a common catalog.
type Manager struct {
	// store persists the selected provider.
	store ProviderStore

	// mu protects the active provider selection.
	mu sync.RWMutex
	// active identifies the selected provider.
	active ProviderID
	// providers contains provider implementations keyed by stable provider ID.
	providers map[ProviderID]Provider
}

// NewManager creates a media manager and restores the active provider.
func NewManager(
	ctx context.Context,
	store ProviderStore,
	providers ...Provider,
) (*Manager, error) {
	registered, err := registerProviders(providers)
	if err != nil {
		return nil, err
	}
	manager := &Manager{store: store, providers: registered}

	active, err := store.LoadMediaProvider(ctx)
	if err == nil {
		if _, ok := registered[active]; !ok {
			return nil, fmt.Errorf("restore media provider %q: %w", active, ErrUnknownProvider)
		}
		manager.active = active
		return manager, nil
	}
	if !errors.Is(err, ErrProviderNotFound) {
		return nil, fmt.Errorf("restore media provider: %w", err)
	}

	// A legacy database can contain provider-specific credentials without the
	// active-provider marker. Recover automatically when exactly one provider is configured.
	configured := configuredProvider(registered)
	if configured != "" {
		if err := store.SaveMediaProvider(ctx, configured); err != nil {
			return nil, fmt.Errorf("persist recovered media provider: %w", err)
		}
		manager.active = configured
	}

	return manager, nil
}

// registerProviders indexes provider implementations by their stable ID.
func registerProviders(providers []Provider) (map[ProviderID]Provider, error) {
	registered := make(map[ProviderID]Provider, len(providers))
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		id := provider.ID()
		if id == "" {
			return nil, ErrUnknownProvider
		}
		if _, exists := registered[id]; exists {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateProvider, id)
		}
		registered[id] = provider
	}
	return registered, nil
}

// configuredProvider returns the only configured provider, or empty when zero or multiple are configured.
func configuredProvider(providers map[ProviderID]Provider) ProviderID {
	var configured ProviderID
	for id, provider := range providers {
		ready, _ := provider.Configured()
		if !ready {
			continue
		}
		if configured != "" {
			return ""
		}
		configured = id
	}
	return configured
}

// CheckProvider verifies that a provider can be configured without switching an active instance.
func (m *Manager) CheckProvider(provider ProviderID) error {
	if _, ok := m.providers[provider]; !ok {
		return ErrUnknownProvider
	}
	m.mu.RLock()
	active := m.active
	m.mu.RUnlock()
	if active != "" && active != provider {
		return ErrProviderConflict
	}
	return nil
}

// SetActive selects a configured provider. Switching to a different configured provider is intentionally rejected.
func (m *Manager) SetActive(ctx context.Context, provider ProviderID) error {
	implementation, ok := m.providers[provider]
	if !ok {
		return ErrUnknownProvider
	}
	configured, _ := implementation.Configured()
	if !configured {
		return ErrNotConfigured
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != "" && m.active != provider {
		return ErrProviderConflict
	}

	if err := m.store.SaveMediaProvider(ctx, provider); err != nil {
		return err
	}
	m.active = provider
	return nil
}

// Status returns the currently active media-server state.
func (m *Manager) Status() Status {
	m.mu.RLock()
	provider := m.active
	m.mu.RUnlock()
	if provider == "" {
		return Status{}
	}
	implementation := m.providers[provider]
	configured, serverName := implementation.Configured()
	return Status{Configured: configured, Provider: provider, ServerName: serverName}
}

// Libraries returns libraries from the active provider.
func (m *Manager) Libraries(ctx context.Context) ([]Library, error) {
	provider, err := m.provider()
	if err != nil {
		return nil, err
	}
	return provider.Libraries(ctx)
}

// Items returns items from the active provider.
func (m *Manager) Items(ctx context.Context, library Library) ([]Item, error) {
	provider, err := m.provider()
	if err != nil {
		return nil, err
	}
	return provider.Items(ctx, library)
}

// Poster retrieves a poster from the active provider.
func (m *Manager) Poster(ctx context.Context, reference string) (*http.Response, error) {
	provider, err := m.provider()
	if err != nil {
		return nil, err
	}
	return provider.Poster(ctx, reference)
}

// provider returns the configured active provider implementation.
func (m *Manager) provider() (Provider, error) {
	m.mu.RLock()
	provider := m.active
	m.mu.RUnlock()
	if provider == "" {
		return nil, ErrNotConfigured
	}
	implementation, ok := m.providers[provider]
	if !ok {
		return nil, ErrUnknownProvider
	}
	configured, _ := implementation.Configured()
	if !configured {
		return nil, ErrNotConfigured
	}
	return implementation, nil
}
