package main

import (
	"bytes"
	"context"
	"log/slog"
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

// result is what one run of the command produced.
type result struct {
	cfg            *server.Config // passed to the server; nil if it didn't start
	err            error
	stdout, stderr string
}

// runDview runs the command with test dependencies and an empty
// environment whose home directory has no config file, and returns the
// config passed to the server, or the error.
func runDview(t *testing.T, cwd string, args ...string) (*server.Config, error) {
	t.Helper()
	r := runWithEnv(t, cwd, map[string]string{"HOME": t.TempDir()}, args...)
	return r.cfg, r.err
}

// runWithEnv runs the command with the given environment variables only.
func runWithEnv(t *testing.T, cwd string, env map[string]string, args ...string) result {
	t.Helper()
	var got *server.Config
	var stdout, stderr bytes.Buffer
	d := deps{
		getwd: func() (string, error) { return cwd, nil },
		lookupEnv: func(name string) (string, bool) {
			v, ok := env[name]
			return v, ok
		},
		resolver: noResolver{},
		ifaceAddrs: func() ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("192.168.1.10")}, nil
		},
		run: func(_ context.Context, cfg server.Config) error {
			got = &cfg
			return nil
		},
		stdout: &stdout,
		stderr: &stderr,
	}
	err := newCommand(d).Run(context.Background(), append([]string{"dview"}, args...))
	return result{cfg: got, err: err, stdout: stdout.String(), stderr: stderr.String()}
}

// writeConfig writes dview.yaml in home/.config and returns home.
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".config"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".config", "dview.yaml"), []byte(content), 0o644))
	return home
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

func TestDebugOption(t *testing.T) {
	cwd := tempDir(t)

	cfg, err := runDview(t, cwd)
	require.NoError(t, err)
	assert.False(t, cfg.Debug)
	assert.False(t, cfg.Log.Enabled(context.Background(), slog.LevelDebug))

	cfg, err = runDview(t, cwd, "--debug")
	require.NoError(t, err)
	assert.True(t, cfg.Debug)
	assert.True(t, cfg.Log.Enabled(context.Background(), slog.LevelDebug))

	// -x was replaced by --debug.
	cfg, err = runDview(t, cwd, "-x")
	require.Error(t, err)
	assert.Nil(t, cfg)
}

func TestURLOption(t *testing.T) {
	cwd := tempDir(t)

	cfg, err := runDview(t, cwd)
	require.NoError(t, err)
	assert.Empty(t, cfg.BackendURL)

	cfg, err = runDview(t, cwd, "--url", "http://127.0.0.1:7860/")
	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1:7860/", cfg.BackendURL)

	cfg, err = runDview(t, cwd, "--url", "127.0.0.1:7860")
	require.ErrorContains(t, err, "invalid --url")
	assert.Nil(t, cfg)
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

func TestSettingsPriority(t *testing.T) {
	cwd := tempDir(t)
	fromFile := filepath.Join(cwd, "fromfile")
	fromEnv1, fromEnv2 := filepath.Join(cwd, "env1"), filepath.Join(cwd, "env2")
	fromCLI := filepath.Join(cwd, "cli")
	for _, d := range []string{fromFile, fromEnv1, fromEnv2, fromCLI} {
		require.NoError(t, os.Mkdir(d, 0o755))
	}
	home := writeConfig(t, "listen: \"1111\"\nurl: http://file:7860/\ndir:\n  - "+fromFile+"\ndebug: true\n")
	names := func(cfg *server.Config) []string {
		var out []string
		for _, r := range cfg.Roots.List {
			out = append(out, r.Name)
		}
		return out
	}

	// The config file alone.
	r := runWithEnv(t, cwd, map[string]string{"HOME": home})
	require.NoError(t, r.err)
	assert.Equal(t, uint16(1111), r.cfg.Listen[0].Port())
	assert.Equal(t, "http://file:7860/", r.cfg.BackendURL)
	assert.Equal(t, []string{"fromfile", filepath.Base(cwd)}, names(r.cfg))
	assert.True(t, r.cfg.Debug)

	// Environment variables override the file.
	env := map[string]string{
		"HOME":         home,
		"DVIEW_LISTEN": "2222",
		"DVIEW_URL":    "http://env:7860/",
		"DVIEW_DIR":    fromEnv1 + string(os.PathListSeparator) + fromEnv2,
		"DVIEW_DEBUG":  "false",
	}
	r = runWithEnv(t, cwd, env)
	require.NoError(t, r.err)
	assert.Equal(t, uint16(2222), r.cfg.Listen[0].Port())
	assert.Equal(t, "http://env:7860/", r.cfg.BackendURL)
	assert.Equal(t, []string{"env1", "env2", filepath.Base(cwd)}, names(r.cfg))
	assert.False(t, r.cfg.Debug)

	// The command line overrides both.
	r = runWithEnv(t, cwd, env, "--listen", "3333", "--url", "http://cli:7860/", "--dir", fromCLI, "--debug")
	require.NoError(t, r.err)
	assert.Equal(t, uint16(3333), r.cfg.Listen[0].Port())
	assert.Equal(t, "http://cli:7860/", r.cfg.BackendURL)
	assert.Equal(t, []string{"cli", filepath.Base(cwd)}, names(r.cfg))
	assert.True(t, r.cfg.Debug)

	// An explicit --debug=false beats the file's debug: true.
	r = runWithEnv(t, cwd, map[string]string{"HOME": home}, "--debug=false")
	require.NoError(t, r.err)
	assert.False(t, r.cfg.Debug)
}

func TestConfigFileSearch(t *testing.T) {
	cwd := tempDir(t)
	home := writeConfig(t, "listen: \"4444\"\n")
	port := func(r result) uint16 {
		t.Helper()
		require.NoError(t, r.err)
		return r.cfg.Listen[0].Port()
	}

	// $HOME/.config/dview.yaml when there's nothing in the working directory.
	assert.Equal(t, uint16(4444), port(runWithEnv(t, cwd, map[string]string{"HOME": home})))

	// ./.dview.yml comes before it, and ./.dview.yaml before that.
	require.NoError(t, os.WriteFile(filepath.Join(cwd, ".dview.yml"), []byte("listen: \"5555\"\n"), 0o644))
	assert.Equal(t, uint16(5555), port(runWithEnv(t, cwd, map[string]string{"HOME": home})))
	require.NoError(t, os.WriteFile(filepath.Join(cwd, ".dview.yaml"), []byte("listen: \"6666\"\n"), 0o644))
	assert.Equal(t, uint16(6666), port(runWithEnv(t, cwd, map[string]string{"HOME": home})))

	// $XDG_CONFIG_HOME comes after the working directory, before $HOME/.config.
	xdg := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(xdg, "dview.yml"), []byte("listen: \"3333\"\n"), 0o644))
	other := tempDir(t)
	assert.Equal(t, uint16(3333), port(runWithEnv(t, other, map[string]string{"HOME": home, "XDG_CONFIG_HOME": xdg})))
	assert.Equal(t, uint16(6666), port(runWithEnv(t, cwd, map[string]string{"HOME": home, "XDG_CONFIG_HOME": xdg})))

	// Only one file is read: the first found.
	require.NoError(t, os.WriteFile(filepath.Join(cwd, ".dview.yaml"), []byte("url: http://x/\n"), 0o644))
	r := runWithEnv(t, cwd, map[string]string{"HOME": home})
	require.NoError(t, r.err)
	assert.Equal(t, uint16(8008), r.cfg.Listen[0].Port(), "the home file's listen is not read")
}

func TestConfigOption(t *testing.T) {
	cwd := tempDir(t)
	home := writeConfig(t, "listen: \"4444\"\n")
	envFile := filepath.Join(cwd, "env.yaml")
	cliFile := filepath.Join(cwd, "cli.yml")
	require.NoError(t, os.WriteFile(envFile, []byte("listen: \"7777\"\n"), 0o644))
	require.NoError(t, os.WriteFile(cliFile, []byte("listen: \"8888\"\n"), 0o644))

	// DVIEW_CONFIG replaces the search.
	env := map[string]string{"HOME": home, "DVIEW_CONFIG": "env.yaml"}
	r := runWithEnv(t, cwd, env)
	require.NoError(t, r.err)
	assert.Equal(t, uint16(7777), r.cfg.Listen[0].Port())

	// --config beats DVIEW_CONFIG.
	r = runWithEnv(t, cwd, env, "--config", cliFile)
	require.NoError(t, r.err)
	assert.Equal(t, uint16(8888), r.cfg.Listen[0].Port())

	// Settings from the environment and command line still beat the file.
	env["DVIEW_LISTEN"] = "9999"
	r = runWithEnv(t, cwd, env, "--config", cliFile)
	require.NoError(t, r.err)
	assert.Equal(t, uint16(9999), r.cfg.Listen[0].Port())

	// A named file that doesn't exist is an error, from either place.
	r = runWithEnv(t, cwd, map[string]string{"HOME": home}, "--config", "nope.yaml")
	require.ErrorContains(t, r.err, "config file")
	assert.Nil(t, r.cfg)
	r = runWithEnv(t, cwd, map[string]string{"HOME": home, "DVIEW_CONFIG": "nope.yaml"})
	require.ErrorContains(t, r.err, "config file")
	assert.Nil(t, r.cfg)

	// config is not a setting in the file.
	bad := filepath.Join(cwd, "bad.yaml")
	require.NoError(t, os.WriteFile(bad, []byte("config: other.yaml\n"), 0o644))
	r = runWithEnv(t, cwd, map[string]string{"HOME": home}, "--config", bad)
	require.Error(t, r.err)
}

func TestSettingsErrors(t *testing.T) {
	cwd := tempDir(t)

	// A typo in the config file is reported, and so is show-config there.
	for _, content := range []string{"lisen: 9000\n", "show-config: true\n", "dir: [\n"} {
		r := runWithEnv(t, cwd, map[string]string{"HOME": writeConfig(t, content)})
		require.Error(t, r.err, content)
		assert.Contains(t, r.err.Error(), "dview.yaml", content)
		assert.Nil(t, r.cfg)
	}

	r := runWithEnv(t, cwd, map[string]string{"HOME": t.TempDir(), "DVIEW_DEBUG": "maybe"})
	require.ErrorContains(t, r.err, "DVIEW_DEBUG")
	assert.Nil(t, r.cfg)
}

func TestShowConfig(t *testing.T) {
	cwd := tempDir(t)
	home := writeConfig(t, "url: http://file:7860/\ndir: /data/outputs\n")
	env := map[string]string{
		"HOME":              home,
		"DVIEW_LISTEN":      "[::]:9000",
		"DVIEW_SHOW_CONFIG": "true", // not a setting: ignored
	}

	r := runWithEnv(t, cwd, env, "--show-config", "--dir", "old=/archive", "--dir", "/data/new")
	require.NoError(t, r.err)
	assert.Nil(t, r.cfg, "the server must not start")
	assert.Empty(t, r.stderr, "no other output")
	assert.Equal(t, "listen: '[::]:9000'\nurl: http://file:7860/\ndir:\n  - old=/archive\n  - /data/new\ndebug: false\n",
		r.stdout)

	// --config and DVIEW_CONFIG choose the file but aren't settings, so they
	// aren't shown.
	named := filepath.Join(cwd, "named.yaml")
	require.NoError(t, os.WriteFile(named, []byte("debug: true\n"), 0o644))
	r2 := runWithEnv(t, cwd, map[string]string{"HOME": t.TempDir(), "DVIEW_CONFIG": named}, "--show-config")
	require.NoError(t, r2.err)
	assert.Equal(t, "listen: localhost:8008\nurl: \"\"\ndir: []\ndebug: true\n", r2.stdout)
	r2 = runWithEnv(t, cwd, map[string]string{"HOME": t.TempDir()}, "--show-config", "--config", named)
	require.NoError(t, r2.err)
	assert.NotContains(t, r2.stdout, "config")

	// The output is itself a valid config file that gives the same settings.
	again := runWithEnv(t, cwd, map[string]string{"HOME": writeConfig(t, r.stdout)}, "--show-config")
	require.NoError(t, again.err)
	assert.Equal(t, r.stdout, again.stdout)

	// With nothing set, it shows the defaults.
	r = runWithEnv(t, cwd, map[string]string{"HOME": t.TempDir()}, "--show-config")
	require.NoError(t, r.err)
	assert.Equal(t, "listen: localhost:8008\nurl: \"\"\ndir: []\ndebug: false\n", r.stdout)

	// Without --show-config, DVIEW_SHOW_CONFIG doesn't show anything.
	r = runWithEnv(t, cwd, map[string]string{"HOME": t.TempDir(), "DVIEW_SHOW_CONFIG": "true"})
	require.NoError(t, r.err)
	assert.NotNil(t, r.cfg)
	assert.Empty(t, r.stdout)
}
