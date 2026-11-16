// Package options parses and validates dview's command-line options.
package options

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

// DefaultListen is the --listen value used when the option isn't given.
const DefaultListen = "localhost:8008"

// ListenFormat describes the --listen syntax for error messages.
const ListenFormat = "[<address>:]<port>, for example 8008, 0.0.0.0:8008, [::]:8008 or myhost:8008"

const defaultHost = "localhost"

// ListenSpec is a parsed --listen value, before any name resolution.
type ListenSpec struct {
	// Host is an IPv4 address, an IPv6 address (without brackets) or a
	// hostname.
	Host string
	Port uint16
}

// ParseListen parses a --listen value of the form [<address>:]<port>.
// An IPv6 address must be in square brackets. If only a port is given, the
// host is localhost.
func ParseListen(s string) (ListenSpec, error) {
	invalid := func(reason string) (ListenSpec, error) {
		return ListenSpec{}, fmt.Errorf("invalid --listen value %q: %s; expected %s", s, reason, ListenFormat)
	}

	if s == "" {
		return invalid("value is empty")
	}

	host, portStr := defaultHost, s
	if strings.HasPrefix(s, "[") {
		end := strings.Index(s, "]")
		if end < 0 {
			return invalid("missing closing ']' after IPv6 address")
		}
		host = s[1:end]
		rest := s[end+1:]
		if !strings.HasPrefix(rest, ":") {
			return invalid("missing ':<port>' after IPv6 address")
		}
		portStr = rest[1:]
		addr, err := netip.ParseAddr(host)
		if err != nil || !addr.Is6() {
			return invalid(fmt.Sprintf("%q is not an IPv6 address", host))
		}
	} else if i := strings.LastIndex(s, ":"); i >= 0 {
		host, portStr = s[:i], s[i+1:]
		if host == "" {
			return invalid("address before ':' is empty")
		}
		if strings.Contains(host, ":") {
			return invalid("an IPv6 address must be in square brackets, as in [::1]:8008")
		}
		if _, err := netip.ParseAddr(host); err != nil && !validHostname(host) {
			return invalid(fmt.Sprintf("%q is not a valid IP address or hostname", host))
		}
	}

	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil || port == 0 {
		return invalid(fmt.Sprintf("%q is not a valid port number (1-65535)", portStr))
	}

	return ListenSpec{Host: host, Port: uint16(port)}, nil
}

// validHostname reports whether s is a syntactically valid DNS hostname.
func validHostname(s string) bool {
	if len(s) > 253 {
		return false
	}
	for _, label := range strings.Split(strings.TrimSuffix(s, "."), ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

// Resolver looks up the addresses of a hostname. *net.Resolver satisfies it.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// InterfaceAddrsFunc returns the IP addresses of the local network
// interfaces.
type InterfaceAddrsFunc func() ([]netip.Addr, error)

// LocalInterfaceAddrs returns the IP addresses of this machine's network
// interfaces.
func LocalInterfaceAddrs() ([]netip.Addr, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	out := make([]netip.Addr, 0, len(addrs))
	for _, a := range addrs {
		var ip net.IP
		switch v := a.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if addr, ok := netip.AddrFromSlice(ip); ok {
			out = append(out, addr.Unmap())
		}
	}
	return out, nil
}

// ResolveListen turns a ListenSpec into the addresses to listen on.
//
//   - localhost listens on [::1] and 127.0.0.1.
//   - The wildcard addresses 0.0.0.0 and [::] are always allowed.
//   - Any other IP address must be on a local network interface.
//   - A hostname is looked up, and every resolved address on a local
//     interface is used. It is an error if the lookup fails or none of the
//     addresses are local.
func ResolveListen(ctx context.Context, spec ListenSpec, r Resolver, ifaceAddrs InterfaceAddrsFunc) ([]netip.AddrPort, error) {
	if strings.EqualFold(strings.TrimSuffix(spec.Host, "."), "localhost") {
		return []netip.AddrPort{
			netip.AddrPortFrom(netip.IPv6Loopback(), spec.Port),
			netip.AddrPortFrom(netip.AddrFrom4([4]byte{127, 0, 0, 1}), spec.Port),
		}, nil
	}

	local, err := ifaceAddrs()
	if err != nil {
		return nil, fmt.Errorf("listing local network interfaces: %w", err)
	}
	isLocal := func(a netip.Addr) bool {
		a = a.WithZone("")
		for _, l := range local {
			if l.WithZone("") == a {
				return true
			}
		}
		return false
	}

	if addr, err := netip.ParseAddr(spec.Host); err == nil {
		addr = addr.Unmap()
		if !addr.IsUnspecified() && !isLocal(addr) {
			return nil, fmt.Errorf("invalid --listen address %s: not on a local network interface", addr)
		}
		return []netip.AddrPort{netip.AddrPortFrom(addr, spec.Port)}, nil
	}

	resolved, err := r.LookupNetIP(ctx, "ip", spec.Host)
	if err != nil {
		return nil, fmt.Errorf("invalid --listen address: looking up %q: %w", spec.Host, err)
	}
	var out []netip.AddrPort
	seen := map[netip.Addr]bool{}
	for _, a := range resolved {
		a = a.Unmap()
		if seen[a] || !(a.IsUnspecified() || isLocal(a)) {
			continue
		}
		seen[a] = true
		out = append(out, netip.AddrPortFrom(a, spec.Port))
	}
	if len(out) == 0 {
		return nil, errors.New("invalid --listen address: none of the addresses for " +
			strconv.Quote(spec.Host) + " are on a local network interface")
	}
	return out, nil
}
