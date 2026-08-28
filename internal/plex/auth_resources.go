package plex

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// SelectServer persists the selected Plex server connection.
func (m *AuthManager) SelectServer(ctx context.Context, setupToken, serverID string) error {
	logger := m.requestLogger(ctx)
	pending := m.pendingAuthSnapshot(setupToken)
	if authorizationIncomplete(pending, m.now()) {
		return ErrAuthorizationIncomplete
	}

	server, ok := pending.resources[serverID]
	if !ok {
		logger.Warn("selected Plex server is unavailable",
			"event", "plex_server_unavailable",
			"server_id", serverID,
		)
		return ErrServerUnavailable
	}
	logServerSelection(logger, server)

	selected, ok := preferredConnection(server.Connections)
	if !ok {
		return ErrNoUsableConnection
	}

	effectiveURL := m.serverURL(selected.URI)
	logSelectedConnection(logger, server, selected, effectiveURL, m.serverURLOverride != "")

	candidates := serverVerificationCandidates(pending.method, pending.userToken, server.AccessToken)
	serverToken, err := m.verifyServer(ctx, effectiveURL, pending.clientID, candidates...)
	if err != nil {
		logger.Error("Plex server verification failed",
			"event", "plex_server_verification_failed",
			"server_id", server.ClientIdentifier,
			"url", safeURL(effectiveURL),
			"error", err,
		)
		return fmt.Errorf("%w: %w", ErrServerVerification, err)
	}

	state := authStateForServer(pending, server, selected, serverToken)
	if err := m.persistSelectedServer(ctx, setupToken, state); err != nil {
		return err
	}

	logger.Info("Plex server selected",
		"event", "plex_server_selected",
		"server_id", state.ServerID,
		"server_name", state.ServerName,
		"effective_url", safeURL(effectiveURL),
	)
	return nil
}

// logServerSelection records the selected Plex resource and its advertised connections.
func logServerSelection(logger *slog.Logger, server resource) {
	logger.Info("selecting Plex server",
		"event", "plex_server_selecting",
		"server_id", server.ClientIdentifier,
		"server_name", server.Name,
		"owned", server.Owned,
		"platform", server.Platform,
		"connection_count", len(server.Connections),
	)

	for _, candidate := range server.Connections {
		logger.Debug("discovered Plex connection",
			"event", "plex_connection_discovered",
			"server_id", server.ClientIdentifier,
			"url", safeURL(candidate.URI),
			"local", candidate.Local,
			"relay", candidate.Relay,
		)
	}
}

// logSelectedConnection records the Plex endpoint selected for runtime use.
func logSelectedConnection(
	logger *slog.Logger,
	server resource,
	selected connection,
	effectiveURL string,
	overridden bool,
) {
	logger.Info("selected Plex connection",
		"event", "plex_connection_selected",
		"server_id", server.ClientIdentifier,
		"discovered_url", safeURL(selected.URI),
		"effective_url", safeURL(effectiveURL),
		"local", selected.Local,
		"relay", selected.Relay,
		"url_override", overridden,
	)
}

// authStateForServer builds persisted authentication state for a selected Plex server.
func authStateForServer(
	pending *pendingAuth,
	server resource,
	selected connection,
	serverToken string,
) AuthState {
	return AuthState{
		Method:         pending.method,
		ClientID:       pending.clientID,
		KeyID:          pending.keyID,
		PrivateKey:     pending.privateKey,
		UserToken:      pending.userToken,
		TokenExpiresAt: pending.tokenExp,
		ServerID:       server.ClientIdentifier,
		ServerName:     server.Name,
		ServerURL:      selected.URI,
		ServerToken:    serverToken,
	}
}

// persistSelectedServer saves authentication state and completes the setup session.
func (m *AuthManager) persistSelectedServer(
	ctx context.Context,
	setupToken string,
	state AuthState,
) error {
	if err := m.store.SavePlexAuth(ctx, state); err != nil {
		return fmt.Errorf("save Plex authentication: %w", err)
	}

	m.mu.Lock()
	m.state = state
	delete(m.pending, setupToken)
	m.mu.Unlock()
	return nil
}

// serverVerificationCandidates returns Plex tokens in method-specific verification order.
func serverVerificationCandidates(
	method AuthMethod,
	accountToken string,
	resourceToken string,
) []tokenCandidate {
	if method == AuthMethodJWT {
		return []tokenCandidate{
			{
				kind:  "account_jwt",
				value: accountToken,
			},
			{
				kind:  "resource_token",
				value: resourceToken,
			},
		}
	}
	return []tokenCandidate{
		{
			kind:  "resource_token",
			value: resourceToken,
		},
		{
			kind:  "account_token",
			value: accountToken,
		},
	}
}

// usableServerResource reports whether a Plex resource can represent a selectable server.
func usableServerResource(item resource) bool {
	return item.ClientIdentifier != "" && containsProvide(item.Provides, "server") && len(item.Connections) > 0
}

// verifyServer returns the first token accepted by the selected Plex server.
func (m *AuthManager) verifyServer(
	ctx context.Context,
	serverURL string,
	clientID string,
	candidates ...tokenCandidate,
) (string, error) {
	logger := m.requestLogger(ctx)
	var failures []error
	seen := make(map[string]struct{}, len(candidates))

	for _, candidate := range candidates {
		if candidate.value == "" {
			logger.Debug("skipping empty Plex token candidate",
				"event", "plex_token_candidate_skipped",
				"token_kind", candidate.kind,
			)
			continue
		}

		if _, exists := seen[candidate.value]; exists {
			logger.Debug("skipping duplicate Plex token candidate",
				"event", "plex_token_candidate_skipped",
				"token_kind", candidate.kind,
			)
			continue
		}
		seen[candidate.value] = struct{}{}

		logger.Info("verifying Plex server access",
			"event", "plex_server_verifying",
			"url", safeURL(serverURL),
			"token_kind", candidate.kind,
		)

		started := time.Now()

		client, err := NewWithClientID(serverURL, candidate.value, clientID)
		if err == nil {
			_, err = client.Libraries(ctx)
		}

		if err == nil {
			logger.Info("Plex server access verified",
				"event", "plex_server_verified",
				"url", safeURL(serverURL),
				"token_kind", candidate.kind,
				"duration_ms", time.Since(started).Milliseconds(),
			)
			return candidate.value, nil
		}
		logger.Warn("Plex server rejected token candidate",
			"event", "plex_server_verification_attempt_failed",
			"url", safeURL(serverURL),
			"token_kind", candidate.kind,
			"duration_ms", time.Since(started).Milliseconds(),
			"error", err,
		)

		failures = append(failures, fmt.Errorf("%s: %w", candidate.kind, err))
	}

	if len(failures) == 0 {
		return "", ErrMissingToken
	}

	return "", errors.Join(failures...)
}

// resources retrieves the Plex resources available to an account.
func (m *AuthManager) resources(
	ctx context.Context,
	method AuthMethod,
	clientID string,
	token string,
) (map[string]resource, error) {
	if method == AuthMethodStandard {
		return m.xmlResources(ctx, clientID, token)
	}

	logger := m.requestLogger(ctx)

	query := url.Values{
		"includeHttps": {"1"},
		"includeRelay": {"1"},
		"includeIPv6":  {"1"},
	}

	var response []resource
	if err := m.cloudJSON(ctx, http.MethodGet, "/api/v2/resources", clientID, token, query, nil, &response); err != nil {
		return nil, err
	}

	resources := make(map[string]resource)
	for _, item := range response {
		if !usableServerResource(item) {
			continue
		}

		resources[item.ClientIdentifier] = item

		logger.Debug("discovered Plex server",
			"event", "plex_server_discovered",
			"server_id", item.ClientIdentifier,
			"server_name", item.Name,
			"owned", item.Owned,
			"platform", item.Platform,
			"connection_count", len(item.Connections),
			"has_resource_token", item.AccessToken != "",
		)
	}

	if len(resources) == 0 {
		return nil, ErrNoServers
	}

	logger.Info("Plex servers discovered",
		"event", "plex_servers_discovered",
		"server_count", len(resources),
	)

	return resources, nil
}

// xmlResources retrieves Plex resources with server-compatible resource tokens.
func (m *AuthManager) xmlResources(
	ctx context.Context,
	clientID string,
	token string,
) (map[string]resource, error) {
	logger := m.requestLogger(ctx)
	query := url.Values{
		"includeHttps": {"1"},
		"includeRelay": {"1"},
		"includeIPv6":  {"1"},
	}

	var response xmlResourcesResponse
	if err := m.cloudXML(ctx, http.MethodGet, "/api/resources", clientID, token, query, &response); err != nil {
		return nil, err
	}

	resources := make(map[string]resource)
	for _, item := range response.Devices {
		converted := resourceFromXML(item)
		if !usableServerResource(converted) {
			continue
		}

		resources[converted.ClientIdentifier] = converted
		logger.Debug("discovered Plex server",
			"event", "plex_server_discovered",
			"server_id", converted.ClientIdentifier,
			"server_name", converted.Name,
			"owned", converted.Owned,
			"platform", converted.Platform,
			"connection_count", len(converted.Connections),
			"has_resource_token", converted.AccessToken != "",
			"auth_method", AuthMethodStandard,
		)
	}
	if len(resources) == 0 {
		return nil, ErrNoServers
	}

	logger.Info("Plex servers discovered",
		"event", "plex_servers_discovered",
		"server_count", len(resources),
		"auth_method", AuthMethodStandard,
	)
	return resources, nil
}

// resourceFromXML converts an XML Plex resource into the common resource model.
func resourceFromXML(item xmlResource) resource {
	connections := make([]connection, 0, len(item.Connections))
	for _, candidate := range item.Connections {
		connections = append(connections, connection{
			URI:   candidate.URI,
			Local: candidate.Local == 1,
			Relay: candidate.Relay == 1,
		})
	}

	return resource{
		Name:             item.Name,
		ClientIdentifier: item.ClientIdentifier,
		Provides:         item.Provides,
		Owned:            item.Owned == 1,
		AccessToken:      item.AccessToken,
		Platform:         item.Platform,
		Connections:      connections,
	}
}

// serverInfos converts Plex resources into selectable server summaries.
func serverInfos(resources map[string]resource) []ServerInfo {
	servers := make([]ServerInfo, 0, len(resources))
	for _, server := range resources {
		selected, _ := preferredConnection(server.Connections)
		servers = append(servers, ServerInfo{
			ID:       server.ClientIdentifier,
			Name:     server.Name,
			Owned:    server.Owned,
			Local:    selected.Local,
			Relay:    selected.Relay,
			Platform: server.Platform,
		})
	}
	slices.SortFunc(servers, func(a, b ServerInfo) int {
		if a.Local != b.Local {
			if a.Local {
				return -1
			}
			return 1
		}
		if a.Owned != b.Owned {
			if a.Owned {
				return -1
			}
			return 1
		}
		return cmp.Compare(a.Name, b.Name)
	})
	return servers
}

// preferredConnection chooses the best reachable Plex connection.
func preferredConnection(connections []connection) (selected connection, ok bool) {
	if len(connections) == 0 {
		return connection{}, false
	}
	copyOf := slices.Clone(connections)
	slices.SortStableFunc(copyOf, func(a, b connection) int {
		score := func(c connection) int {
			if c.Local && !c.Relay {
				return 0
			}
			if !c.Relay {
				return 1
			}
			return 2
		}
		return cmp.Compare(score(a), score(b))
	})
	for _, candidate := range copyOf {
		if _, err := parseHTTPURL(candidate.URI); err != nil {
			continue
		}
		return candidate, true
	}
	return connection{}, false
}

// containsProvide reports whether a Plex resource offers a capability.
func containsProvide(provides, wanted string) bool {
	for value := range strings.SplitSeq(provides, ",") {
		if strings.TrimSpace(value) == wanted {
			return true
		}
	}
	return false
}
