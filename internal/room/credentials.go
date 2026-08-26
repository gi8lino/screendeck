package room

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
)

// cleanName normalizes and limits a participant name.
func cleanName(name string) string {
	name = strings.Join(
		strings.Fields(strings.TrimSpace(name)),
		" ",
	)

	runes := []rune(name)

	if len(runes) > 30 {
		name = string(runes[:30])
	}

	return name
}

// roomCode creates a short random room identifier.
func roomCode() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

	bytes := make([]byte, 6)

	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	for i := range bytes {
		bytes[i] = alphabet[int(bytes[i])%len(alphabet)]
	}

	return string(bytes), nil
}

// credentials creates a participant identifier and authentication token.
func credentials() (
	id,
	token,
	tokenHash string,
	err error,
) {
	raw := make([]byte, 32)

	if _, err = rand.Read(raw); err != nil {
		return "", "", "", err
	}

	token = base64.RawURLEncoding.EncodeToString(raw)

	hash := sha256.Sum256(raw)

	id = hex.EncodeToString(hash[:12])
	tokenHash = hex.EncodeToString(hash[:])

	return
}

// hashToken returns the stored digest of a participant token.
func hashToken(token string) string {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return "invalid"
	}

	hash := sha256.Sum256(raw)

	return hex.EncodeToString(hash[:])
}
