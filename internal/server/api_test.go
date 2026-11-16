package server

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/png"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dview/internal/diff"
	"dview/internal/options"
)

func writePNG(t *testing.T, path, params string) {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewGray(image.Rect(0, 0, 2, 2))))
	b := buf.Bytes()
	if params != "" {
		data := []byte("parameters\x00" + params)
		var c bytes.Buffer
		_ = binary.Write(&c, binary.BigEndian, uint32(len(data)))
		c.WriteString("tEXt")
		c.Write(data)
		_ = binary.Write(&c, binary.BigEndian, crc32.ChecksumIEEE(append([]byte("tEXt"), data...)))
		b = append(append(append([]byte{}, b[:33]...), c.Bytes()...), b[33:]...)
	}
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, b, 0o644))
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	writePNG(t, filepath.Join(root, "out/a.png"),
		"a red fox\nNegative prompt: blurry\nSteps: 30, Sampler: Euler, CFG scale: 7")
	writePNG(t, filepath.Join(root, "out/b.png"),
		"a grey fox\nNegative prompt: blurry\nCFG scale: 5, Steps: 30, Sampler: Euler")
	writePNG(t, filepath.Join(root, "out/none.png"), "")
	require.NoError(t, os.WriteFile(filepath.Join(root, "out/bad.png"), []byte("not a png"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "secret.png"), []byte("x"), 0o644))

	srv := httptest.NewServer(Handler(Config{
		Roots: options.Roots{List: []options.Root{{Name: "out", Path: filepath.Join(root, "out")}}},
	}))
	t.Cleanup(srv.Close)
	return srv
}

func getJSON(t *testing.T, srv *httptest.Server, path string, v any) int {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	require.NoError(t, json.NewDecoder(resp.Body).Decode(v))
	return resp.StatusCode
}

func TestAPIList(t *testing.T) {
	srv := newTestServer(t)
	var l struct {
		Path    string `json:"path"`
		Entries []struct {
			Name string `json:"name"`
		} `json:"entries"`
	}
	require.Equal(t, http.StatusOK, getJSON(t, srv, "/api/list?path=/", &l))
	var got []string
	for _, e := range l.Entries {
		got = append(got, e.Name)
	}
	assert.Equal(t, []string{"a.png", "b.png", "bad.png", "none.png"}, got)

	var e map[string]string
	assert.Equal(t, http.StatusNotFound, getJSON(t, srv, "/api/list?path=/missing", &e))
	assert.NotEmpty(t, e["error"])
	assert.Equal(t, http.StatusBadRequest, getJSON(t, srv, "/api/list?path=relative", &e))
}

func TestAPIImage(t *testing.T) {
	srv := newTestServer(t)
	resp, err := http.Get(srv.URL + "/api/image?path=/a.png")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "image/png", resp.Header.Get("Content-Type"))

	for path, want := range map[string]int{
		"/missing.png":                http.StatusNotFound,
		"/../secret.png":              http.StatusNotFound, // can't climb out of the tree
		"/" + url.PathEscape("a.txt"): http.StatusBadRequest,
	} {
		resp, err := http.Get(srv.URL + "/api/image?path=" + path)
		require.NoError(t, err)
		resp.Body.Close()
		assert.Equal(t, want, resp.StatusCode, path)
	}
}

func TestAPICompare(t *testing.T) {
	srv := newTestServer(t)
	var c compareResponse
	require.Equal(t, http.StatusOK, getJSON(t, srv, "/api/compare?a=/a.png&b=/b.png", &c))
	require.NotNil(t, c.A)
	require.NotNil(t, c.B)
	assert.Equal(t, "a.png", c.A.Display)
	assert.Equal(t, "a red fox", c.A.Params.Prompt)
	require.NotNil(t, c.Diff)
	assert.Equal(t, "a[- red-]{+ grey+} fox", showSegs(c.Diff.Prompt))
	assert.Len(t, c.Diff.Settings, 4) // Steps, Sampler, -CFG, +CFG

	// One image: no diff.
	c = compareResponse{}
	require.Equal(t, http.StatusOK, getJSON(t, srv, "/api/compare?b=/b.png", &c))
	assert.Nil(t, c.A)
	assert.Nil(t, c.Diff)

	// Missing and unreadable metadata: no diff.
	c = compareResponse{}
	require.Equal(t, http.StatusOK, getJSON(t, srv, "/api/compare?a=/none.png&b=/bad.png", &c))
	assert.Equal(t, "missing", c.A.Status)
	assert.Equal(t, "unreadable", c.B.Status)
	assert.NotEmpty(t, c.B.Error)
	assert.Nil(t, c.Diff)

	var e map[string]string
	assert.Equal(t, http.StatusNotFound, getJSON(t, srv, "/api/compare?a=/missing.png", &e))
}

func showSegs(segs []diff.Segment) string {
	var sb strings.Builder
	for _, s := range segs {
		switch s.Op {
		case diff.Insert:
			sb.WriteString("{+" + s.Text + "+}")
		case diff.Delete:
			sb.WriteString("[-" + s.Text + "-]")
		default:
			sb.WriteString(s.Text)
		}
	}
	return sb.String()
}

func TestAPIConfigAndClientLog(t *testing.T) {
	// Without debug logging, the page is told not to send messages, and
	// messages are refused.
	plain := httptest.NewServer(Handler(Config{}))
	defer plain.Close()
	var cfg map[string]bool
	require.Equal(t, http.StatusOK, getJSON(t, plain, "/api/config", &cfg))
	assert.False(t, cfg["debug"])
	resp, err := http.Post(plain.URL+"/api/log", "application/json", strings.NewReader(`{"msg":"x"}`))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	// With debug logging, browser messages go to the log.
	var log bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&log, &slog.HandlerOptions{Level: slog.LevelDebug}))
	dbg := httptest.NewServer(Handler(Config{Log: logger, Debug: true}))
	defer dbg.Close()
	require.Equal(t, http.StatusOK, getJSON(t, dbg, "/api/config", &cfg))
	assert.True(t, cfg["debug"])

	resp, err = http.Post(dbg.URL+"/api/log", "application/json",
		strings.NewReader(`{"level":"error","msg":"boom","data":{"line":12}}`))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Contains(t, log.String(), `level=ERROR msg=boom source=browser line=12`)

	resp, err = http.Post(dbg.URL+"/api/log", "application/json", strings.NewReader(`not json`))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}
