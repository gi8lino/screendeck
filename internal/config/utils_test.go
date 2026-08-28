package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestValidAbsoluteHTTPURL verifies Plex URL overrides require an absolute HTTP or HTTPS URL.
func TestValidAbsoluteHTTPURL(t *testing.T) {
	t.Run("http", func(t *testing.T) {
		assert.True(t, validAbsoluteHTTPURL("http://plex.test:32400"))
	})

	t.Run("https", func(t *testing.T) {
		assert.True(t, validAbsoluteHTTPURL("https://plex.test"))
	})

	t.Run("missing scheme", func(t *testing.T) {
		assert.False(t, validAbsoluteHTTPURL("plex.test:32400"))
	})

	t.Run("unsupported scheme", func(t *testing.T) {
		assert.False(t, validAbsoluteHTTPURL("ftp://plex.test"))
	})
}

// TestListenAddressIsLoopback verifies network-exposure detection for startup warnings.
func TestListenAddressIsLoopback(t *testing.T) {
	t.Run("IPv4 loopback", func(t *testing.T) {
		assert.True(t, ListenAddressIsLoopback("127.0.0.1:8080"))
	})

	t.Run("IPv6 loopback", func(t *testing.T) {
		assert.True(t, ListenAddressIsLoopback("[::1]:8080"))
	})

	t.Run("all interfaces", func(t *testing.T) {
		assert.False(t, ListenAddressIsLoopback(":8080"))
	})

	t.Run("LAN address", func(t *testing.T) {
		assert.False(t, ListenAddressIsLoopback("192.168.1.10:8080"))
	})
}
