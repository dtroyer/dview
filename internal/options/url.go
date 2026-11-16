package options

import (
	"fmt"
	"net/url"
)

// ParseBackendURL checks a --url value: an http or https URL with a host. An
// empty value means no URL was given.
func ParseBackendURL(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", fmt.Errorf("invalid --url %q: %w", s, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("invalid --url %q: must start with http:// or https://", s)
	}
	if u.Host == "" {
		return "", fmt.Errorf("invalid --url %q: no host", s)
	}
	return u.String(), nil
}
