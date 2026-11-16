package options

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tree creates directories under a temporary root and returns the root with
// symbolic links resolved (macOS puts temp dirs behind /var -> /private/var).
func tree(t *testing.T, dirs ...string) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	for _, d := range dirs {
		require.NoError(t, os.MkdirAll(filepath.Join(root, d), 0o755))
	}
	return root
}

func TestParseDirsWorkingDirOnly(t *testing.T) {
	root := tree(t, "work")
	cwd := filepath.Join(root, "work")

	got, err := ParseDirs(cwd, nil)
	require.NoError(t, err)
	assert.Equal(t, Roots{List: []Root{{Name: "work", Path: cwd}}}, got)
	assert.False(t, got.Virtual())
}

func TestParseDirsVirtualRoot(t *testing.T) {
	root := tree(t, "work", "data/outputs", "archive/outputs", "rel")
	cwd := filepath.Join(root, "work")

	got, err := ParseDirs(cwd, []string{
		filepath.Join(root, "data/outputs"),
		"old=" + filepath.Join(root, "archive/outputs"),
		"../rel",
	})
	require.NoError(t, err)
	assert.Equal(t, Roots{List: []Root{
		{Name: "outputs", Path: filepath.Join(root, "data/outputs")},
		{Name: "old", Path: filepath.Join(root, "archive/outputs")},
		{Name: "rel", Path: filepath.Join(root, "rel")},
		{Name: "work", Path: cwd},
	}}, got)
	assert.True(t, got.Virtual())
}

func TestParseDirsResolvesSymlinks(t *testing.T) {
	root := tree(t, "work", "real")
	cwd := filepath.Join(root, "work")
	require.NoError(t, os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "link")))

	got, err := ParseDirs(cwd, []string{filepath.Join(root, "link")})
	require.NoError(t, err)
	// The name comes from the path as given; the path is the real one.
	assert.Equal(t, Root{Name: "link", Path: filepath.Join(root, "real")}, got.List[0])
}

func TestParseDirsSameDirectoryTwice(t *testing.T) {
	root := tree(t, "work")
	cwd := filepath.Join(root, "work")

	got, err := ParseDirs(cwd, []string{".", cwd})
	require.NoError(t, err)
	assert.Equal(t, []Root{{Name: "work", Path: cwd}}, got.List)
}

func TestParseDirsWholeFilesystem(t *testing.T) {
	root := tree(t, "work")
	cwd := filepath.Join(root, "work")

	got, err := ParseDirs(cwd, []string{"/"})
	require.NoError(t, err)
	assert.True(t, got.WholeFS)
	assert.False(t, got.Virtual())
	assert.Equal(t, []Root{{Name: "", Path: "/"}, {Name: "work", Path: cwd}}, got.List)

	// A named "/" is an ordinary virtual root entry.
	got, err = ParseDirs(cwd, []string{"all=/"})
	require.NoError(t, err)
	assert.False(t, got.WholeFS)
	assert.True(t, got.Virtual())
	assert.Equal(t, []Root{{Name: "all", Path: "/"}, {Name: "work", Path: cwd}}, got.List)
}

func TestParseDirsPathWithEquals(t *testing.T) {
	root := tree(t, "work", "a=b")
	cwd := filepath.Join(root, "work")

	// Without a name, the text before '=' is taken as the name.
	_, err := ParseDirs(cwd, []string{filepath.Join(root, "a=b")})
	require.Error(t, err)

	got, err := ParseDirs(cwd, []string{"ab=" + filepath.Join(root, "a=b")})
	require.NoError(t, err)
	assert.Equal(t, Root{Name: "ab", Path: filepath.Join(root, "a=b")}, got.List[0])
}

func TestParseDirsErrors(t *testing.T) {
	root := tree(t, "work", "a/outputs", "b/outputs", "work2/work")
	cwd := filepath.Join(root, "work")
	file := filepath.Join(root, "file.png")
	require.NoError(t, os.WriteFile(file, nil, 0o644))

	tests := map[string]struct {
		specs   []string
		wantMsg string
	}{
		"missing":         {[]string{filepath.Join(root, "nope")}, "no such file or directory"},
		"not a directory": {[]string{file}, "is not a directory"},
		"empty name":      {[]string{"=" + root}, "must not be empty"},
		"slash in name":   {[]string{"a/b=" + root}, "contain '/'"},
		"empty path":      {[]string{"name="}, "path is empty"},
		"empty value":     {[]string{""}, "path is empty"},
		"duplicate base names": {
			[]string{filepath.Join(root, "a/outputs"), filepath.Join(root, "b/outputs")},
			"both have the name \"outputs\"",
		},
		"duplicate explicit names": {
			[]string{"x=" + filepath.Join(root, "a"), "x=" + filepath.Join(root, "b")},
			"both have the name \"x\"",
		},
		"clashes with working directory": {
			[]string{filepath.Join(root, "work2/work")},
			"both have the name \"work\"",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := ParseDirs(cwd, tt.specs)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
}

func TestParseDirsDuplicateSuggestsNameForm(t *testing.T) {
	root := tree(t, "work", "a/outputs", "b/outputs")
	_, err := ParseDirs(filepath.Join(root, "work"),
		[]string{filepath.Join(root, "a/outputs"), filepath.Join(root, "b/outputs")})
	require.ErrorContains(t, err, "--dir <name>=<path>")
}
