package plex

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// cloudJSON sends a JSON request to the Plex cloud API.
func (m *AuthManager) cloudJSON(
	ctx context.Context,
	method string,
	path string,
	clientID string,
	token string,
	query url.Values,
	body any,
	target any,
) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode Plex authentication request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	response, err := m.cloudRequest(ctx, method, path, clientID, token, query, reader, "application/json", "application/json")
	if err != nil {
		return err
	}
	defer response.Body.Close() // nolint:errcheck

	if target == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(target); err != nil {
		return fmt.Errorf("%w: %s %s: %w", ErrCloudDecode, method, path, err)
	}
	return nil
}

// cloudXML sends an XML request to the Plex XML cloud API.
func (m *AuthManager) cloudXML(
	ctx context.Context,
	method string,
	path string,
	clientID string,
	token string,
	query url.Values,
	target any,
) error {
	response, err := m.cloudRequest(ctx, method, path, clientID, token, query, nil, "application/xml", "")
	if err != nil {
		return err
	}
	defer response.Body.Close() // nolint:errcheck

	if err := xml.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(target); err != nil {
		return fmt.Errorf("%w: %s %s: %w", ErrCloudDecode, method, path, err)
	}
	return nil
}

// cloudRequest sends one authenticated request to the Plex cloud API.
func (m *AuthManager) cloudRequest(
	ctx context.Context,
	method string,
	path string,
	clientID string,
	token string,
	query url.Values,
	body io.Reader,
	accept string,
	contentType string,
) (*http.Response, error) {
	logger := m.requestLogger(ctx)
	started := time.Now()
	logger.Debug("sending Plex authentication request",
		"event", "plex_cloud_request",
		"method", method,
		"path", path,
		"authenticated", token != "",
	)

	u := m.cloudBase.JoinPath(path)
	u.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, fmt.Errorf("create Plex authentication request: %w", err)
	}
	setPlexCloudHeaders(req, clientID, token, accept, contentType)

	response, err := m.httpClient.Do(req)
	if err != nil {
		logger.Error("Plex authentication request failed",
			"event", "plex_cloud_request_failed",
			"method", method,
			"path", path,
			"duration_ms", time.Since(started).Milliseconds(),
			"error", err,
		)
		return nil, fmt.Errorf("%w: %s %s: %w", ErrCloudUnavailable, method, path, err)
	}

	logger.Debug("Plex authentication response received",
		"event", "plex_cloud_response",
		"method", method,
		"path", path,
		"status", response.StatusCode,
		"duration_ms", time.Since(started).Milliseconds(),
	)
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return response, nil
	}

	defer response.Body.Close() // nolint:errcheck
	message, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
	return nil, fmt.Errorf(
		"%w: %s %s: %s: %s",
		ErrCloudResponse,
		method,
		path,
		response.Status,
		strings.TrimSpace(string(message)),
	)
}

// setPlexCloudHeaders adds the common headers required by Plex cloud requests.
func setPlexCloudHeaders(
	req *http.Request,
	clientID string,
	token string,
	accept string,
	contentType string,
) {
	req.Header.Set("Accept", accept)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("X-Plex-Product", "ScreenDeck")
	req.Header.Set("X-Plex-Version", "1.0")
	req.Header.Set("X-Plex-Client-Identifier", clientID)
	if token != "" {
		req.Header.Set("X-Plex-Token", token)
	}
}
