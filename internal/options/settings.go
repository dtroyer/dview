package options

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Settings are dview's options that can come from the config file,
// environment variables or the command line. From lowest to highest
// priority: the built-in defaults, the config file, DVIEW_* environment
// variables, then the command line.
type Settings struct {
	Listen string     `yaml:"listen"`
	URL    string     `yaml:"url"`
	Dir    StringList `yaml:"dir"`
	Debug  bool       `yaml:"debug"`
}

// DefaultSettings returns the built-in defaults.
func DefaultSettings() Settings {
	return Settings{Listen: DefaultListen, Dir: StringList{}}
}

// StringList is a list of strings that may also be written as a single
// string in YAML.
type StringList []string

// UnmarshalYAML accepts a list of strings or a single string.
func (l *StringList) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		if n.Tag == "!!null" {
			*l = StringList{}
			return nil
		}
		*l = StringList{n.Value}
		return nil
	}
	var list []string
	if err := n.Decode(&list); err != nil {
		return err
	}
	*l = list
	return nil
}

// EnvConfig names a config file to load instead of searching for one.
const EnvConfig = "DVIEW_CONFIG"

// ConfigCandidates returns the config files to look for, in order, when
// none is named: .dview.yaml or .dview.yml in the working directory, then
// dview.yaml or dview.yml in $XDG_CONFIG_HOME (when it is set), then in
// $HOME/.config.
func ConfigCandidates(cwd string, lookup func(string) (string, bool)) []string {
	out := []string{filepath.Join(cwd, ".dview.yaml"), filepath.Join(cwd, ".dview.yml")}
	addDir := func(dir string) {
		for _, p := range []string{filepath.Join(dir, "dview.yaml"), filepath.Join(dir, "dview.yml")} {
			if !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	if xdg, ok := lookup("XDG_CONFIG_HOME"); ok && xdg != "" {
		addDir(xdg)
	}
	home, ok := lookup("HOME")
	if !ok || home == "" {
		home, _ = os.UserHomeDir()
	}
	if home != "" {
		addDir(filepath.Join(home, ".config"))
	}
	return out
}

// FindConfigFile returns the config file to load. A named file (from
// --config or DVIEW_CONFIG) is used as given, relative to cwd, and must
// exist. Otherwise the first of ConfigCandidates that exists is used, and
// "" means there is none.
func FindConfigFile(named, cwd string, lookup func(string) (string, bool)) (string, error) {
	if named != "" {
		path := expandHome(named)
		if !filepath.IsAbs(path) {
			path = filepath.Join(cwd, path)
		}
		info, err := os.Stat(path)
		if err != nil {
			return "", fmt.Errorf("config file: %w", err)
		}
		if info.IsDir() {
			return "", fmt.Errorf("config file %s is a directory", path)
		}
		return path, nil
	}
	for _, path := range ConfigCandidates(cwd, lookup) {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", nil
}

// LoadConfigFile reads settings from a YAML config file onto s. Unknown
// settings are errors, to catch typos.
func LoadConfigFile(path string, s *Settings) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading config file: %w", err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(s); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("config file %s: %w", path, err)
	}
	return nil
}

// Environment variables, one per setting. DVIEW_DIR holds one or more
// --dir values separated by the path list separator (":" on Linux and
// macOS), like PATH.
const (
	EnvListen = "DVIEW_LISTEN"
	EnvURL    = "DVIEW_URL"
	EnvDir    = "DVIEW_DIR"
	EnvDebug  = "DVIEW_DEBUG"
)

// ApplyEnv overrides s with the DVIEW_* environment variables that are set.
// An empty variable counts as not set.
func ApplyEnv(s *Settings, lookup func(string) (string, bool)) error {
	get := func(name string) (string, bool) {
		v, ok := lookup(name)
		return v, ok && v != ""
	}
	if v, ok := get(EnvListen); ok {
		s.Listen = v
	}
	if v, ok := get(EnvURL); ok {
		s.URL = v
	}
	if v, ok := get(EnvDir); ok {
		var dirs StringList
		for _, d := range filepath.SplitList(v) {
			if d != "" {
				dirs = append(dirs, d)
			}
		}
		s.Dir = dirs
	}
	if v, ok := get(EnvDebug); ok {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("invalid %s %q: use true or false", EnvDebug, v)
		}
		s.Debug = b
	}
	return nil
}

// Marshal returns s as the contents of a config file.
func (s Settings) Marshal() ([]byte, error) {
	if s.Dir == nil {
		s.Dir = StringList{}
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(s); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// expandHome replaces a leading "~" or "~/" with the home directory. The
// shell does this on the command line, but not in the config file or
// environment variables.
func expandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~"))
}
