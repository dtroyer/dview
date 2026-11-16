package main

import (
	"bytes"
	"context"
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dview/internal/options"
	"dview/internal/server"
)

type noResolver struct{}

func (noResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return nil, assert.AnError
}

// runDview runs the command with test dependencies and returns the config
// passed to the server, or the error.
func runDview(t *testing.T, cwd string, args ...string) (*server.Config, error) {
	t.Helper()
	var got *server.Config
	var out bytes.Buffer
	d := deps{
		getwd:    func() (string, error) { return cwd, nil },
		resolver: noResolver{},
		ifaceAddrs: func() ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("192.168.1.10")}, nil
		},
		run: func(_ context.Context, cfg server.Config) error {
			got = &cfg
			return nil
		},
		stdout: &out,
		stderr: &out,
	}
	err := newCommand(d).Run(context.Background(), append([]string{"dview"}, args...))
	return got, err
}

func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return dir
}

func TestDefaults(t *testing.T) {
	cwd := tempDir(t)
	cfg, err := runDview(t, cwd)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, []netip.AddrPort{
		netip.MustParseAddrPort("[::1]:8008"),
		netip.MustParseAddrPort("127.0.0.1:8008"),
	}, cfg.Listen)
	assert.Equal(t, []options.Root{{Name: filepath.Base(cwd), Path: cwd}}, cfg.Roots.List)
}

func TestListenOption(t *testing.T) {
	cwd := tempDir(t)

	cfg, err := runDview(t, cwd, "--listen", "9000")
	require.NoError(t, err)
	assert.Equal(t, []netip.AddrPort{
		netip.MustParseAddrPort("[::1]:9000"),
		netip.MustParseAddrPort("127.0.0.1:9000"),
	}, cfg.Listen)

	cfg, err = runDview(t, cwd, "--listen", "[::]:8008")
	require.NoError(t, err)
	assert.Equal(t, []netip.AddrPort{netip.MustParseAddrPort("[::]:8008")}, cfg.Listen)

	cfg, err = runDview(t, cwd, "--listen=192.168.1.10:80")
	require.NoError(t, err)
	assert.Equal(t, []netip.AddrPort{netip.MustParseAddrPort("192.168.1.10:80")}, cfg.Listen)
}

func TestListenOptionErrors(t *testing.T) {
	cwd := tempDir(t)
	for _, v := range []string{"abc", "::1:80", "10.9.9.9:80", "nosuchhost:80"} {
		t.Run(v, func(t *testing.T) {
			cfg, err := runDview(t, cwd, "--listen", v)
			require.Error(t, err)
			assert.Nil(t, cfg, "the server must not start")
		})
	}
}

func TestDirOptionRepeatable(t *testing.T) {
	cwd := tempDir(t)
	a := filepath.Join(cwd, "a,b") // a comma must not split the value
	b := filepath.Join(cwd, "b")
	require.NoError(t, os.Mkdir(a, 0o755))
	require.NoError(t, os.Mkdir(b, 0o755))

	cfg, err := runDview(t, cwd, "--dir", a, "--dir", "bee="+b)
	require.NoError(t, err)
	assert.Equal(t, []options.Root{
		{Name: "a,b", Path: a},
		{Name: "bee", Path: b},
		{Name: filepath.Base(cwd), Path: cwd},
	}, cfg.Roots.List)
	assert.True(t, cfg.Roots.Virtual())
}

func TestDirOptionError(t *testing.T) {
	cwd := tempDir(t)
	cfg, err := runDview(t, cwd, "--dir", filepath.Join(cwd, "missing"))
	require.Error(t, err)
	assert.Nil(t, cfg)
}

func TestRejectsArguments(t *testing.T) {
	cfg, err := runDview(t, tempDir(t), "image.png")
	require.ErrorContains(t, err, "unexpected argument")
	assert.Nil(t, cfg)
}
