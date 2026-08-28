package plex

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// Refresh renews Plex authentication when necessary.
func (m *AuthManager) Refresh(ctx context.Context) error {
	return m.refresh(ctx, true)
}

// refresh renews and persists Plex authentication state.
func (m *AuthManager) refresh(ctx context.Context, force bool) error {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()

	logger := m.requestLogger(ctx)
	state := m.authStateSnapshot()
	if state.UserToken == "" {
		return ErrAuthNotFound
	}
	if state.Method != AuthMethodJWT {
		logger.Debug("Plex token refresh is not required for standard authentication",
			"event", "plex_token_refresh_skipped",
			"auth_method", state.Method,
		)
		return nil
	}
	if !force && !tokenNeedsRefresh(state.TokenExpiresAt, m.now()) {
		logger.Debug("Plex token refresh not required",
			"event", "plex_token_refresh_skipped",
			"token_expires_at", state.TokenExpiresAt,
		)
		return nil
	}

	logger.Info("refreshing Plex authentication",
		"event", "plex_token_refreshing",
		"server_id", state.ServerID,
		"token_expires_at", state.TokenExpiresAt,
	)

	accountToken, err := m.refreshAccountToken(ctx, state)
	if err != nil {
		return err
	}

	state, effectiveURL, err := m.refreshServerAccess(ctx, logger, state, accountToken)
	if err != nil {
		return err
	}
	if err := m.store.SavePlexAuth(ctx, state); err != nil {
		return fmt.Errorf("save refreshed Plex authentication: %w", err)
	}
	m.setAuthState(state)

	logger.Info("Plex authentication refreshed",
		"event", "plex_token_refreshed",
		"server_id", state.ServerID,
		"effective_url", safeURL(effectiveURL),
		"token_expires_at", state.TokenExpiresAt,
	)
	return nil
}

// refreshAccountToken requests a new Plex account JWT for the active device key.
func (m *AuthManager) refreshAccountToken(ctx context.Context, state AuthState) (string, error) {
	var nonceResponse authNonceResponse
	if err := m.cloudJSON(ctx, http.MethodGet, "/api/v2/auth/nonce", state.ClientID, "", nil, nil, &nonceResponse); err != nil {
		return "", err
	}

	now := m.now()
	deviceJWT, err := signDeviceJWT(state.PrivateKey, state.KeyID, map[string]any{
		"nonce": nonceResponse.Nonce,
		"scope": "friendly_name",
		"aud":   "plex.tv",
		"iss":   state.ClientID,
		"iat":   now.Unix(),
		"exp":   now.Add(5 * time.Minute).Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("sign Plex refresh JWT: %w", err)
	}

	var tokenResponse authTokenResponse
	if err := m.cloudJSON(
		ctx,
		http.MethodPost,
		"/api/v2/auth/token",
		state.ClientID,
		"",
		nil,
		tokenRefreshRequest{JWT: deviceJWT},
		&tokenResponse,
	); err != nil {
		return "", err
	}
	if tokenResponse.AuthToken == "" {
		return "", ErrEmptyRefreshedToken
	}
	return tokenResponse.AuthToken, nil
}

// refreshServerAccess verifies the refreshed account token and returns updated server state.
func (m *AuthManager) refreshServerAccess(
	ctx context.Context,
	logger *slog.Logger,
	state AuthState,
	accountToken string,
) (refreshedState AuthState, effectiveURL string, err error) {
	resources, err := m.resources(ctx, AuthMethodJWT, state.ClientID, accountToken)
	if err != nil {
		return AuthState{}, "", err
	}

	resourceToken := ""
	if server, ok := resources[state.ServerID]; ok {
		if selected, found := preferredConnection(server.Connections); found {
			state.ServerURL = selected.URI
		}
		resourceToken = server.AccessToken
	}

	effectiveURL = m.serverURL(state.ServerURL)
	serverToken, err := m.verifyServer(
		ctx,
		effectiveURL,
		state.ClientID,
		tokenCandidate{kind: "account_jwt", value: accountToken},
		tokenCandidate{kind: "resource_token", value: resourceToken},
	)
	if err != nil {
		logger.Error("refreshed Plex token verification failed",
			"event", "plex_token_refresh_verification_failed",
			"server_id", state.ServerID,
			"url", safeURL(effectiveURL),
			"error", err,
		)
		return AuthState{}, "", fmt.Errorf("%w: %w", ErrAuthenticationRefresh, err)
	}

	state.UserToken = accountToken
	state.ServerToken = serverToken
	state.TokenExpiresAt = tokenExpiry(accountToken, m.now().Add(7*24*time.Hour))

	refreshedState = state
	return refreshedState, effectiveURL, nil
}

// tokenNeedsRefresh reports whether a JWT should be refreshed before normal use.
func tokenNeedsRefresh(expiresAt, now time.Time) bool {
	return expiresAt.IsZero() || expiresAt.Sub(now) < 12*time.Hour
}

// authenticationRefreshNeeded reports whether the active authentication state needs JWT refresh.
func authenticationRefreshNeeded(state AuthState, now time.Time) bool {
	return state.Method == AuthMethodJWT && tokenNeedsRefresh(state.TokenExpiresAt, now)
}
