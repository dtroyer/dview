package fsview

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dview/internal/options"
)

// tree creates files and directories under a temporary root (with symbolic
// links resolved) and returns the root. Names ending in "/" are directories.
func tree(t *testing.T, names ...string) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	for _, n := range names {
		p := filepath.Join(root, n)
		if n[len(n)-1] == '/' {
			require.NoError(t, os.MkdirAll(p, 0o755))
			continue
		}
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte("x"), 0o644))
	}
	return root
}

func names(l Listing) []string {
	var out []string
	for _, e := range l.Entries {
		n := e.Name
		if e.Dir {
			n += "/"
		}
		out = append(out, n)
	}
	return out
}

func TestListSingleRoot(t *testing.T) {
	root := tree(t, "work/b.png", "work/A.PNG", "work/img10.jpg", "work/img2.webp", "work/notes.txt",
		"work/.hidden.png", "work/.git/", "work/sub/", "work/Zdir/", "work/x.jpeg")
	cwd := filepath.Join(root, "work")
	f := New(options.Roots{List: []options.Root{{Name: "work", Path: cwd}}}, nil)

	l, err := f.List("/")
	require.NoError(t, err)
	assert.Equal(t, "/", l.Path)
	assert.Equal(t, ".", l.Display)
	assert.Empty(t, l.Parent, "no .. at the top of the tree")
	// Directories first, then images, each in plain character order.
	assert.Equal(t, []string{"Zdir/", "sub/", "A.PNG", "b.png", "img10.jpg", "img2.webp", "x.jpeg"}, names(l))
	assert.Equal(t, "img10.jpg", l.Entries[4].Display)

	l, err = f.List("/sub")
	require.NoError(t, err)
	assert.Equal(t, "/", l.Parent)
	assert.Equal(t, "sub", l.Display)
	assert.Empty(t, l.Entries)
	assert.NotNil(t, l.Entries, "an empty directory lists as [] in JSON")
}

func TestListVirtualRoot(t *testing.T) {
	root := tree(t, "data/outputs/a.png", "work/w.png")
	f := New(options.Roots{List: []options.Root{
		{Name: "outputs", Path: filepath.Join(root, "data/outputs")},
		{Name: "work", Path: filepath.Join(root, "work")},
	}}, nil)

	l, err := f.List("")
	require.NoError(t, err)
	assert.Equal(t, []string{"outputs/", "work/"}, names(l))
	assert.Equal(t, "/outputs", l.Entries[0].Path)

	l, err = f.List("/outputs")
	require.NoError(t, err)
	assert.Equal(t, "/", l.Parent)
	assert.Equal(t, "/outputs", l.Display)
	assert.Equal(t, "/outputs/a.png", l.Entries[0].Path)

	_, err = f.List("/nope")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestListWholeFS(t *testing.T) {
	root := tree(t, "a/b.png")
	f := New(options.Roots{WholeFS: true, List: []options.Root{{Path: "/"}}}, nil)

	l, err := f.List(filepath.ToSlash(filepath.Join(root, "a")))
	require.NoError(t, err)
	assert.Equal(t, filepath.ToSlash(filepath.Join(root, "a")), l.Display)
	assert.Equal(t, []string{"b.png"}, names(l))
}

func TestCleanStaysInsideTree(t *testing.T) {
	for in, want := range map[string]string{
		"":               "/",
		"/":              "/",
		"/a/../../..":    "/",
		"/a/./b//c/":     "/a/b/c",
		"/../etc/passwd": "/etc/passwd",
	} {
		got, err := Clean(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	for _, in := range []string{"relative", "../x", "/a\x00b"} {
		_, err := Clean(in)
		assert.ErrorIs(t, err, ErrInvalidPath, in)
	}
}

func TestSymlinks(t *testing.T) {
	root := tree(t, "work/real.png", "work/dir/in.png", "other/o.png", "outside/secret.png")
	work, other := filepath.Join(root, "work"), filepath.Join(root, "other")
	link := func(target, name string) {
		require.NoError(t, os.Symlink(target, filepath.Join(work, name)))
	}
	link(filepath.Join(root, "outside"), "out-dir")
	link(filepath.Join(root, "outside/secret.png"), "out.png")
	link(filepath.Join(work, "dir"), "in-dir")
	link(filepath.Join(other, "o.png"), "to-other.png")
	link(filepath.Join(root, "missing.png"), "broken.png")

	f := New(options.Roots{List: []options.Root{
		{Name: "other", Path: other},
		{Name: "work", Path: work},
	}}, nil)

	l, err := f.List("/work")
	require.NoError(t, err)
	// Links that lead outside the allowed directories, or nowhere, are hidden.
	assert.Equal(t, []string{"dir/", "in-dir/", "real.png", "to-other.png"}, names(l))

	_, err = f.List("/work/out-dir")
	assert.ErrorIs(t, err, ErrNotAllowed)
	_, err = f.OpenImage("/work/out.png")
	assert.ErrorIs(t, err, ErrNotAllowed)
	_, err = f.OpenImage("/work/broken.png")
	assert.ErrorIs(t, err, ErrNotFound)

	// A link into another top-level directory is followed.
	real, err := f.OpenImage("/work/to-other.png")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(other, "o.png"), real)

	l, err = f.List("/work/in-dir")
	require.NoError(t, err)
	assert.Equal(t, []string{"in.png"}, names(l))
}

func TestOpenImage(t *testing.T) {
	root := tree(t, "work/a.png", "work/notes.txt", "work/d.png/")
	work := filepath.Join(root, "work")
	f := New(options.Roots{List: []options.Root{{Name: "work", Path: work}}}, nil)

	real, err := f.OpenImage("/a.png")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(work, "a.png"), real)

	_, err = f.OpenImage("/notes.txt")
	assert.ErrorIs(t, err, ErrNotImage)
	_, err = f.OpenImage("/d.png")
	assert.ErrorIs(t, err, ErrNotImage, "a directory named like an image")
	_, err = f.OpenImage("/missing.png")
	assert.ErrorIs(t, err, ErrNotFound)
	// ".." can't climb out of the tree.
	_, err = f.OpenImage("/../work/a.png")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestListNotDir(t *testing.T) {
	root := tree(t, "work/a.png")
	f := New(options.Roots{List: []options.Root{{Name: "work", Path: filepath.Join(root, "work")}}}, nil)
	_, err := f.List("/a.png")
	assert.ErrorIs(t, err, ErrNotDir)
}
