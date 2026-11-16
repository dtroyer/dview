package options

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func env(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func TestConfigCandidates(t *testing.T) {
	assert.Equal(t, []string{
		"/work/.dview.yaml", "/work/.dview.yml",
		"/home/me/.config/dview.yaml", "/home/me/.config/dview.yml",
	}, ConfigCandidates("/work", env(map[string]string{"HOME": "/home/me"})))

	// XDG_CONFIG_HOME is searched before $HOME/.config.
	assert.Equal(t, []string{
		"/work/.dview.yaml", "/work/.dview.yml",
		"/xdg/dview.yaml", "/xdg/dview.yml",
		"/home/me/.config/dview.yaml", "/home/me/.config/dview.yml",
	}, ConfigCandidates("/work", env(map[string]string{"HOME": "/home/me", "XDG_CONFIG_HOME": "/xdg"})))

	// Set to the usual place, it isn't listed twice.
	assert.Len(t, ConfigCandidates("/work",
		env(map[string]string{"HOME": "/home/me", "XDG_CONFIG_HOME": "/home/me/.config"})), 4)
}

func TestFindConfigFile(t *testing.T) {
	cwd, home := t.TempDir(), t.TempDir()
	lookup := env(map[string]string{"HOME": home})
	touch := func(p string) {
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte("debug: true\n"), 0o644))
	}

	got, err := FindConfigFile("", cwd, lookup)
	require.NoError(t, err)
	assert.Empty(t, got, "no config file anywhere is fine")

	// Search order: the working directory first, .yaml before .yml.
	order := []string{
		filepath.Join(home, ".config", "dview.yml"),
		filepath.Join(home, ".config", "dview.yaml"),
		filepath.Join(cwd, ".dview.yml"),
		filepath.Join(cwd, ".dview.yaml"),
	}
	for _, p := range order { // add them from lowest to highest priority
		touch(p)
		got, err := FindConfigFile("", cwd, lookup)
		require.NoError(t, err)
		assert.Equal(t, p, got)
	}

	// A named file wins over the search, relative to the working directory.
	named := filepath.Join(cwd, "sub", "mine.yaml")
	touch(named)
	got, err = FindConfigFile("sub/mine.yaml", cwd, lookup)
	require.NoError(t, err)
	assert.Equal(t, named, got)

	// A named file must exist and be a file.
	_, err = FindConfigFile("missing.yaml", cwd, lookup)
	require.Error(t, err)
	_, err = FindConfigFile("sub", cwd, lookup)
	require.ErrorContains(t, err, "is a directory")
}

func TestLoadConfigFile(t *testing.T) {
	dir := t.TempDir()
	write := func(content string) string {
		p := filepath.Join(dir, "dview.yaml")
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
		return p
	}

	s := DefaultSettings()
	require.Error(t, LoadConfigFile(filepath.Join(dir, "missing.yaml"), &s))

	s = DefaultSettings()
	require.NoError(t, LoadConfigFile(write(""), &s))
	assert.Equal(t, DefaultSettings(), s, "an empty file changes nothing")

	s = DefaultSettings()
	require.NoError(t, LoadConfigFile(write("url: http://x/\ndir: /one\n"), &s))
	assert.Equal(t, Settings{Listen: DefaultListen, URL: "http://x/", Dir: StringList{"/one"}}, s,
		"unset keys keep their defaults; a single dir may be a plain string")

	s = DefaultSettings()
	require.NoError(t, LoadConfigFile(write("listen: \"9000\"\ndir:\n  - a\n  - n=b\ndebug: yes\n"), &s))
	assert.Equal(t, Settings{Listen: "9000", Dir: StringList{"a", "n=b"}, Debug: true}, s)

	s = DefaultSettings()
	err := LoadConfigFile(write("listn: 9000\n"), &s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "listn")
}

func TestApplyEnv(t *testing.T) {
	s := Settings{Listen: "1", URL: "u", Dir: StringList{"d"}, Debug: true}
	require.NoError(t, ApplyEnv(&s, env(map[string]string{
		"DVIEW_LISTEN": "2",
		"DVIEW_DIR":    "a" + string(os.PathListSeparator) + "n=b" + string(os.PathListSeparator),
		"DVIEW_DEBUG":  "0",
		"DVIEW_URL":    "", // empty counts as not set
	})))
	assert.Equal(t, Settings{Listen: "2", URL: "u", Dir: StringList{"a", "n=b"}, Debug: false}, s)

	require.Error(t, ApplyEnv(&s, env(map[string]string{"DVIEW_DEBUG": "sometimes"})))
}

func TestMarshalRoundTrip(t *testing.T) {
	in := Settings{Listen: "[::]:8008", URL: "http://h:7860/", Dir: StringList{"/a", "x=/b,c"}, Debug: true}
	out, err := in.Marshal()
	require.NoError(t, err)
	p := filepath.Join(t.TempDir(), "dview.yaml")
	require.NoError(t, os.WriteFile(p, out, 0o644))
	var back Settings
	require.NoError(t, LoadConfigFile(p, &back))
	assert.Equal(t, in, back)
}

func TestDirExpandsHome(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, "pics"), 0o755))
	cwd := filepath.Join(home, "work")
	require.NoError(t, os.MkdirAll(cwd, 0o755))

	roots, err := ParseDirs(cwd, []string{"~/pics", "h=~"})
	require.NoError(t, err)
	assert.Equal(t, []Root{
		{Name: "pics", Path: filepath.Join(home, "pics")},
		{Name: "h", Path: home},
		{Name: "work", Path: cwd},
	}, roots.List)
}
