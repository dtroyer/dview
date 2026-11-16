package options

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DirFormat describes the --dir syntax for error messages.
const DirFormat = "[<name>=]<path>"

// Root is one top-level directory that the file dialog may show.
type Root struct {
	// Name is the directory's name in the virtual root. It is empty for an
	// unnamed "/", which allows the whole filesystem.
	Name string
	// Path is the absolute directory path with symbolic links resolved.
	Path string
}

// Roots is the set of top-level directories that dview may serve files from.
type Roots struct {
	// List holds the top-level directories: the --dir directories in
	// command-line order, then the working directory.
	List []Root
	// WholeFS is true when an unnamed "/" was given, which allows the whole
	// filesystem and turns off the virtual root.
	WholeFS bool
}

// Virtual reports whether the file dialog shows a virtual root listing the
// top-level directories.
func (r Roots) Virtual() bool {
	return !r.WholeFS && len(r.List) > 1
}

// ParseDirs builds the top-level directories from the working directory and
// the --dir values. A relative path is relative to cwd.
func ParseDirs(cwd string, specs []string) (Roots, error) {
	var roots Roots
	byName := map[string]string{} // name -> label of the directory that claimed it
	byPath := map[string]string{} // path -> name, to drop exact repeats

	// label describes the directory in error messages.
	add := func(label, name, path string, explicitName bool) error {
		if !filepath.IsAbs(path) {
			path = filepath.Join(cwd, path)
		}
		path = filepath.Clean(path)
		if !explicitName {
			name = filepath.Base(path)
		}

		real, err := filepath.EvalSymlinks(path)
		if err != nil {
			return fmt.Errorf("invalid %s: %w", label, err)
		}
		info, err := os.Stat(real)
		if err != nil {
			return fmt.Errorf("invalid %s: %w", label, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("invalid %s: %s is not a directory", label, path)
		}

		if !explicitName && real == string(filepath.Separator) {
			roots.WholeFS = true
			name = ""
		}

		if prev, ok := byPath[real]; ok && prev == name {
			return nil // the same directory under the same name; nothing to add
		}
		if name != "" {
			if other, ok := byName[name]; ok {
				return fmt.Errorf("%s and %s both have the name %q; give one a different name with --dir <name>=<path>",
					other, label, name)
			}
			byName[name] = label
		}
		byPath[real] = name
		roots.List = append(roots.List, Root{Name: name, Path: real})
		return nil
	}

	for _, spec := range specs {
		name, path, explicit := strings.Cut(spec, "=")
		if !explicit {
			name, path = "", spec
		} else if name == "" || strings.Contains(name, "/") {
			return Roots{}, fmt.Errorf("invalid --dir %q: name %q must not be empty or contain '/'; expected %s",
				spec, name, DirFormat)
		}
		if path == "" {
			return Roots{}, fmt.Errorf("invalid --dir %q: path is empty; expected %s", spec, DirFormat)
		}
		if err := add(fmt.Sprintf("--dir %q", spec), name, path, explicit); err != nil {
			return Roots{}, err
		}
	}

	if err := add(fmt.Sprintf("working directory %q", cwd), "", cwd, false); err != nil {
		return Roots{}, err
	}
	return roots, nil
}
