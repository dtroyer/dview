package options

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseBackendURL(t *testing.T) {
	for _, in := range []string{
		"http://127.0.0.1:7860/",
		"http://localhost:7860",
		"https://forge.example.com/ui/",
		"http://[::1]:7860/",
	} {
		got, err := ParseBackendURL(in)
		require.NoError(t, err, in)
		assert.Equal(t, in, got)
	}

	got, err := ParseBackendURL("")
	require.NoError(t, err)
	assert.Empty(t, got)

	for in, msg := range map[string]string{
		"127.0.0.1:7860":   "invalid --url",
		"ftp://host/":      "http:// or https://",
		"http://":          "no host",
		"http://host:port": "invalid --url",
		"forge":            "http:// or https://",
	} {
		_, err := ParseBackendURL(in)
		require.Error(t, err, in)
		assert.Contains(t, err.Error(), msg, in)
	}
}
