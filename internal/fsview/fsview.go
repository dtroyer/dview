// Package fsview presents the allowed top-level directories as one tree for
// the file dialog, and maps the dialog's paths back to real files while
// enforcing the access rules.
//
// Paths in the tree always start with "/", which is the top of the tree:
//
//   - With one top-level directory, "/" is that directory (the working
//     directory) and paths are displayed relative to it.
//   - With several, "/" is a virtual root listing them as "/<name>".
//   - With an unnamed --dir /, "/" is the real filesystem root.
package fsview

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"dview/internal/options"
)

// Errors returned for paths the dialog can't use.
var (
	ErrInvalidPath = errors.New("invalid path")
	ErrNotFound    = errors.New("not found")
	ErrNotAllowed  = errors.New("outside the allowed directories")
	ErrNotDir      = errors.New("not a directory")
	ErrNotImage    = errors.New("not an image file")
)

type mode int

const (
	single mode = iota
	virtual
	wholeFS
)

// FS is the tree of allowed directories.
type FS struct {
	roots options.Roots
	mode  mode
}

// New builds the tree for the given top-level directories.
func New(roots options.Roots) *FS {
	m := single
	switch {
	case roots.WholeFS:
		m = wholeFS
	case roots.Virtual():
		m = virtual
	}
	return &FS{roots: roots, mode: m}
}

// Entry is one item in a directory listing.
type Entry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Display string `json:"display"`
	Dir     bool   `json:"dir"`
}

// Listing is the content of one directory in the tree.
type Listing struct {
	Path    string `json:"path"`
	Display string `json:"display"`
	// Parent is the path of the parent directory, or empty at the top of the
	// tree, where moving up isn't allowed.
	Parent  string  `json:"parent,omitempty"`
	Entries []Entry `json:"entries"`
}

var imageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".webp": true}

// IsImageName reports whether name has the extension of a supported image
// format, in any letter case.
func IsImageName(name string) bool {
	return imageExts[strings.ToLower(filepath.Ext(name))]
}

// Clean normalizes a tree path. An empty path means the top of the tree.
// ".." elements are resolved lexically, so they can't climb above "/".
func Clean(p string) (string, error) {
	if p == "" {
		return "/", nil
	}
	if !strings.HasPrefix(p, "/") || strings.ContainsRune(p, 0) {
		return "", ErrInvalidPath
	}
	return path.Clean(p), nil
}

// Display returns how a tree path is shown to the user.
func (f *FS) Display(p string) string {
	if f.mode != single {
		return p
	}
	if rel := strings.TrimPrefix(p, "/"); rel != "" {
		return rel
	}
	return "."
}

// realPath maps a clean tree path to a filesystem path, before symbolic links
// are resolved. top is true for the virtual root, which has no real path.
func (f *FS) realPath(p string) (real string, top bool, err error) {
	switch f.mode {
	case wholeFS:
		return filepath.FromSlash(p), false, nil
	case single:
		return filepath.Join(f.roots.List[0].Path, filepath.FromSlash(p)), false, nil
	}
	if p == "/" {
		return "", true, nil
	}
	name, rest, _ := strings.Cut(strings.TrimPrefix(p, "/"), "/")
	for _, r := range f.roots.List {
		if r.Name == name {
			return filepath.Join(r.Path, filepath.FromSlash(rest)), false, nil
		}
	}
	return "", false, ErrNotFound
}

// allowed reports whether a resolved real path is inside one of the
// top-level directories.
func (f *FS) allowed(real string) bool {
	if f.mode == wholeFS {
		return true
	}
	for _, r := range f.roots.List {
		if r.Path == string(filepath.Separator) || real == r.Path ||
			strings.HasPrefix(real, r.Path+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// resolve follows symbolic links and checks that the result is allowed.
func (f *FS) resolve(real string) (string, fs.FileInfo, error) {
	resolved, err := filepath.EvalSymlinks(real)
	if err != nil {
		return "", nil, ErrNotFound
	}
	if !f.allowed(resolved) {
		return "", nil, ErrNotAllowed
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", nil, ErrNotFound
	}
	return resolved, info, nil
}

// OpenImage returns the real path of the image file at tree path p.
func (f *FS) OpenImage(p string) (string, error) {
	p, err := Clean(p)
	if err != nil {
		return "", err
	}
	if !IsImageName(p) {
		return "", ErrNotImage
	}
	real, top, err := f.realPath(p)
	if err != nil {
		return "", err
	}
	if top {
		return "", ErrNotImage
	}
	resolved, info, err := f.resolve(real)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", ErrNotImage
	}
	return resolved, nil
}

// List returns the directories and image files in the directory at tree
// path p. Hidden entries, symbolic links that lead outside the allowed
// directories or nowhere, and other file types are left out. Directories
// come first; each group is sorted by name in plain character order. At the
// virtual root, the top-level directories are listed in command-line order.
func (f *FS) List(p string) (Listing, error) {
	p, err := Clean(p)
	if err != nil {
		return Listing{}, err
	}
	l := Listing{Path: p, Display: f.Display(p), Entries: []Entry{}}
	if p != "/" {
		l.Parent = path.Dir(p)
	}

	real, top, err := f.realPath(p)
	if err != nil {
		return Listing{}, err
	}
	if top {
		for _, r := range f.roots.List {
			l.Entries = append(l.Entries, Entry{Name: r.Name, Path: "/" + r.Name, Display: "/" + r.Name, Dir: true})
		}
		return l, nil
	}

	dir, info, err := f.resolve(real)
	if err != nil {
		return Listing{}, err
	}
	if !info.IsDir() {
		return Listing{}, ErrNotDir
	}
	items, err := os.ReadDir(dir)
	if err != nil {
		return Listing{}, ErrNotFound
	}

	var dirs, files []Entry
	for _, item := range items {
		name := item.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		isDir, isFile := item.IsDir(), item.Type().IsRegular()
		if item.Type()&fs.ModeSymlink != 0 {
			_, target, err := f.resolve(filepath.Join(dir, name))
			if err != nil {
				continue
			}
			isDir, isFile = target.IsDir(), target.Mode().IsRegular()
		}
		ep := path.Join(p, name)
		e := Entry{Name: name, Path: ep, Display: f.Display(ep), Dir: isDir}
		switch {
		case isDir:
			dirs = append(dirs, e)
		case isFile && IsImageName(name):
			files = append(files, e)
		}
	}
	byName := func(s []Entry) { sort.Slice(s, func(i, j int) bool { return s[i].Name < s[j].Name }) }
	byName(dirs)
	byName(files)
	l.Entries = append(append(l.Entries, dirs...), files...)
	return l, nil
}
