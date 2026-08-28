package config

import (
	"net"
	"net/url"
	"strings"
)

// validAbsoluteHTTPURL reports whether rawURL is an absolute HTTP or HTTPS URL.
func validAbsoluteHTTPURL(rawURL string) bool {
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return false
	}
	return parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https")
}

// ListenAddressIsLoopback reports whether the server is bound only to loopback.
func ListenAddressIsLoopback(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
