package config

import (
	"net/url"
)

// validAbsoluteHTTPURL reports whether rawURL is an absolute HTTP or HTTPS URL.
func validAbsoluteHTTPURL(rawURL string) bool {
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return false
	}
	return parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https")
}
