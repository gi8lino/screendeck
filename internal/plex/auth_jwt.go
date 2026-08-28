package plex

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// buildAuthorizationRequest prepares the method-specific Plex PIN request.
func buildAuthorizationRequest(method AuthMethod) (authorizationRequest, error) {
	request := authorizationRequest{query: url.Values{}}
	if method != AuthMethodJWT {
		request.query.Set("strong", "true")
		return request, nil
	}

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return authorizationRequest{}, fmt.Errorf("generate Plex device key: %w", err)
	}

	digest := sha256.Sum256(publicKey)
	keyID := base64.RawURLEncoding.EncodeToString(digest[:12])
	request.keyID = keyID
	request.privateKey = privateKey
	request.body = authorizationPINRequest{
		JWK: &deviceJWK{
			KeyType:   "OKP",
			Curve:     "Ed25519",
			X:         base64.RawURLEncoding.EncodeToString(publicKey),
			KeyID:     keyID,
			Use:       "sig",
			Algorithm: "EdDSA",
		},
		Strong: true,
	}

	return request, nil
}

// authorizationStatusQuery builds the method-specific PIN polling parameters.
func authorizationStatusQuery(pending *pendingAuth, now time.Time) (url.Values, error) {
	query := url.Values{}
	if pending.method != AuthMethodJWT {
		return query, nil
	}

	deviceJWT, err := signDeviceJWT(
		pending.privateKey,
		pending.keyID,
		map[string]any{
			"aud": "plex.tv",
			"iss": pending.clientID,
			"iat": now.Unix(),
			"exp": now.Add(5 * time.Minute).Unix(),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("sign Plex device JWT: %w", err)
	}

	query.Set("deviceJWT", deviceJWT)
	return query, nil
}

// authorizationTokenExpiry returns the stored account-token expiry for an authorization method.
func authorizationTokenExpiry(method AuthMethod, token string, now time.Time) time.Time {
	if method != AuthMethodJWT {
		return time.Time{}
	}
	return tokenExpiry(token, now.Add(7*24*time.Hour))
}

// signDeviceJWT signs Plex device claims as an EdDSA JWT.
func signDeviceJWT(privateKey ed25519.PrivateKey, keyID string, claims map[string]any) (string, error) {
	header, err := json.Marshal(map[string]string{"alg": "EdDSA", "kid": keyID, "typ": "JWT"})
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	signature := ed25519.Sign(privateKey, []byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

// tokenExpiry extracts a JWT expiration time or returns a fallback.
func tokenExpiry(token string, fallback time.Time) time.Time {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return fallback
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fallback
	}
	var claims tokenClaims
	if json.Unmarshal(payload, &claims) != nil || claims.ExpiresAt == 0 {
		return fallback
	}
	return time.Unix(claims.ExpiresAt, 0).UTC()
}
