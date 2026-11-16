package options

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseListen(t *testing.T) {
	tests := []struct {
		in   string
		want ListenSpec
	}{
		{"8008", ListenSpec{Host: "localhost", Port: 8008}},
		{"9000", ListenSpec{Host: "localhost", Port: 9000}},
		{"localhost:8008", ListenSpec{Host: "localhost", Port: 8008}},
		{"0.0.0.0:8008", ListenSpec{Host: "0.0.0.0", Port: 8008}},
		{"127.0.0.1:8080", ListenSpec{Host: "127.0.0.1", Port: 8080}},
		{"[::]:8008", ListenSpec{Host: "::", Port: 8008}},
		{"[::1]:1", ListenSpec{Host: "::1", Port: 1}},
		{"[fe80::1%eth0]:8008", ListenSpec{Host: "fe80::1%eth0", Port: 8008}},
		{"my-host.example.com:65535", ListenSpec{Host: "my-host.example.com", Port: 65535}},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseListen(tt.in)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseListenInvalid(t *testing.T) {
	tests := map[string]string{
		"":               "empty",
		"abc":            "not a valid port",
		"0":              "not a valid port",
		"65536":          "not a valid port",
		"-1":             "not a valid port",
		"localhost:":     "not a valid port",
		":8008":          "empty",
		"::1:8008":       "square brackets",
		"[::1]8008":      "missing ':<port>'",
		"[::1:8008":      "missing closing ']'",
		"[127.0.0.1]:80": "not an IPv6 address",
		"bad_host:8008":  "not a valid IP address or hostname",
		"-bad.host:8008": "not a valid IP address or hostname",
		"a..b:8008":      "not a valid IP address or hostname",
		"localhost:80x":  "not a valid port",
	}
	for in, wantMsg := range tests {
		t.Run(in, func(t *testing.T) {
			_, err := ParseListen(in)
			require.Error(t, err)
			assert.Contains(t, err.Error(), wantMsg)
			assert.Contains(t, err.Error(), ListenFormat, "error should show the expected format")
		})
	}
}

type fakeResolver struct {
	addrs map[string][]netip.Addr
}

func (f fakeResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	if a, ok := f.addrs[host]; ok {
		return a, nil
	}
	return nil, errors.New("no such host")
}

func addrs(s ...string) []netip.Addr {
	out := make([]netip.Addr, len(s))
	for i, a := range s {
		out[i] = netip.MustParseAddr(a)
	}
	return out
}

func addrPorts(port uint16, s ...string) []netip.AddrPort {
	out := make([]netip.AddrPort, len(s))
	for i, a := range addrs(s...) {
		out[i] = netip.AddrPortFrom(a, port)
	}
	return out
}

func TestResolveListen(t *testing.T) {
	local := func() ([]netip.Addr, error) {
		return addrs("127.0.0.1", "::1", "192.168.1.10", "fe80::1%eth0", "2001:db8::10"), nil
	}
	r := fakeResolver{addrs: map[string][]netip.Addr{
		"myhost":    addrs("192.168.1.10", "2001:db8::10"),
		"mixed":     addrs("10.9.9.9", "192.168.1.10", "::ffff:192.168.1.10"),
		"elsewhere": addrs("10.9.9.9", "2001:db8::99"),
	}}

	tests := []struct {
		host string
		want []netip.AddrPort
	}{
		{"localhost", addrPorts(8008, "::1", "127.0.0.1")},
		{"LOCALHOST", addrPorts(8008, "::1", "127.0.0.1")},
		{"0.0.0.0", addrPorts(8008, "0.0.0.0")},
		{"::", addrPorts(8008, "::")},
		{"192.168.1.10", addrPorts(8008, "192.168.1.10")},
		{"::1", addrPorts(8008, "::1")},
		{"fe80::1%eth0", addrPorts(8008, "fe80::1%eth0")},
		{"myhost", addrPorts(8008, "192.168.1.10", "2001:db8::10")},
		// Non-local addresses are skipped and IPv4-mapped duplicates dropped.
		{"mixed", addrPorts(8008, "192.168.1.10")},
	}
	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			got, err := ResolveListen(context.Background(), ListenSpec{Host: tt.host, Port: 8008}, r, local)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	errTests := map[string]string{
		"10.9.9.9":     "not on a local network interface",
		"2001:db8::99": "not on a local network interface",
		"unknown":      "looking up",
		"elsewhere":    "none of the addresses",
	}
	for host, wantMsg := range errTests {
		t.Run("error/"+host, func(t *testing.T) {
			_, err := ResolveListen(context.Background(), ListenSpec{Host: host, Port: 8008}, r, local)
			require.Error(t, err)
			assert.Contains(t, err.Error(), wantMsg)
		})
	}
}

func TestResolveListenInterfaceError(t *testing.T) {
	failing := func() ([]netip.Addr, error) { return nil, errors.New("boom") }
	_, err := ResolveListen(context.Background(), ListenSpec{Host: "0.0.0.0", Port: 1}, fakeResolver{}, failing)
	require.ErrorContains(t, err, "boom")

	// localhost doesn't need the interface list.
	got, err := ResolveListen(context.Background(), ListenSpec{Host: "localhost", Port: 1}, fakeResolver{}, failing)
	require.NoError(t, err)
	assert.Len(t, got, 2)
}
