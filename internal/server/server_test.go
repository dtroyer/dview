package server

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNetwork(t *testing.T) {
	assert.Equal(t, "tcp", network(netip.MustParseAddr("::")))
	assert.Equal(t, "tcp4", network(netip.MustParseAddr("0.0.0.0")))
	assert.Equal(t, "tcp4", network(netip.MustParseAddr("127.0.0.1")))
	assert.Equal(t, "tcp6", network(netip.MustParseAddr("::1")))
	assert.Equal(t, "tcp6", network(netip.MustParseAddr("fe80::1%lo0")))
}

func TestListenNoAddresses(t *testing.T) {
	_, err := Listen(context.Background(), nil)
	require.Error(t, err)
}

func TestListenFailureClosesOthers(t *testing.T) {
	busy, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer busy.Close()
	busyAddr := netip.MustParseAddrPort(busy.Addr().String())

	_, err = Listen(context.Background(), []netip.AddrPort{
		netip.MustParseAddrPort("127.0.0.1:0"),
		busyAddr,
	})
	require.ErrorContains(t, err, "listening on "+busyAddr.String())
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(body)
}

func TestRunServesUntilCancelled(t *testing.T) {
	addrs := []netip.AddrPort{netip.MustParseAddrPort("127.0.0.1:0")}
	if l, err := net.Listen("tcp6", "[::1]:0"); err == nil {
		l.Close()
		addrs = append(addrs, netip.MustParseAddrPort("[::1]:0"))
	}

	listeners, err := Listen(context.Background(), addrs)
	require.NoError(t, err)
	require.Len(t, listeners, len(addrs))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, listeners, Handler(Config{Log: io.Discard})) }()

	for _, l := range listeners {
		code, body := get(t, "http://"+l.Addr().String()+"/")
		assert.Equal(t, http.StatusOK, code)
		assert.Contains(t, body, "<title>dView</title>")
	}

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not return after cancel")
	}

	for _, l := range listeners {
		_, err := net.DialTimeout("tcp", l.Addr().String(), time.Second)
		assert.Error(t, err, "listener %s should be closed", l.Addr())
	}
}

func TestRunLogsAddresses(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // stop as soon as the listeners are up
	var log bytes.Buffer
	err := Run(ctx, Config{
		Listen: []netip.AddrPort{netip.MustParseAddrPort("127.0.0.1:0")},
		Log:    &log,
	})
	require.NoError(t, err)
	assert.Contains(t, log.String(), "listening on http://127.0.0.1:")
}
