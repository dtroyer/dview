package server

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"

	"dview/internal/diff"
	"dview/internal/fsview"
	"dview/internal/meta"
)

// api serves the JSON and image endpoints used by the web UI.
type api struct {
	fs    *fsview.FS
	log   *slog.Logger
	debug bool
}

// maxClientLog bounds the size of a message the browser can send to the log.
const maxClientLog = 16 << 10

// config handles GET /api/config, which tells the page whether to send its
// own log messages to the server.
func (a *api) config(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"debug": a.debug})
}

// clientLog handles POST /api/log: a log message from the browser. It is
// only available with debug logging on.
func (a *api) clientLog(w http.ResponseWriter, r *http.Request) {
	if !a.debug {
		http.NotFound(w, r)
		return
	}
	var m struct {
		Level string         `json:"level"`
		Msg   string         `json:"msg"`
		Data  map[string]any `json:"data"`
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxClientLog))
	if err == nil {
		err = json.Unmarshal(body, &m)
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid log message"})
		return
	}
	level := slog.LevelDebug
	switch m.Level {
	case "error":
		level = slog.LevelError
	case "warn":
		level = slog.LevelWarn
	}
	args := []any{"source", "browser"}
	for k, v := range m.Data {
		args = append(args, k, v)
	}
	a.log.Log(r.Context(), level, m.Msg, args...)
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, fsview.ErrInvalidPath), errors.Is(err, fsview.ErrNotDir), errors.Is(err, fsview.ErrNotImage):
		status = http.StatusBadRequest
	case errors.Is(err, fsview.ErrNotAllowed):
		status = http.StatusForbidden
	case errors.Is(err, fsview.ErrNotFound):
		status = http.StatusNotFound
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

// list handles GET /api/list?path=<dir>.
func (a *api) list(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	l, err := a.fs.List(p)
	if err != nil {
		a.log.Debug("list failed", "path", p, "err", err)
		writeError(w, err)
		return
	}
	a.log.Debug("listed directory", "path", l.Path, "entries", len(l.Entries))
	writeJSON(w, http.StatusOK, l)
}

// image handles GET /api/image?path=<file>.
func (a *api) image(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	real, err := a.fs.OpenImage(p)
	if err != nil {
		a.log.Debug("image refused", "path", p, "err", err)
		writeError(w, err)
		return
	}
	a.log.Debug("serving image", "path", p, "file", real)
	f, err := os.Open(real)
	if err != nil {
		writeError(w, fsview.ErrNotFound)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeError(w, fsview.ErrNotFound)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, path.Base(real), info.ModTime(), f)
}

// side describes one image in a comparison.
type side struct {
	Path    string `json:"path"`
	Display string `json:"display"`
	Name    string `json:"name"`
	// Params is nil when the metadata is missing or unreadable.
	Params *meta.Params `json:"params"`
	// Status is "ok", "missing" or "unreadable".
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type comparison struct {
	Prompt   []diff.Segment `json:"prompt"`
	Negative []diff.Segment `json:"negative"`
	Settings []diff.Line    `json:"settings"`
}

type compareResponse struct {
	A *side `json:"a"`
	B *side `json:"b"`
	// Diff compares B against A. It is nil unless both have metadata.
	Diff *comparison `json:"diff"`
}

func (a *api) loadSide(p string) (*side, error) {
	p, err := fsview.Clean(p)
	if err != nil {
		return nil, err
	}
	real, err := a.fs.OpenImage(p)
	if err != nil {
		return nil, err
	}
	s := &side{Path: p, Display: a.fs.Display(p), Name: path.Base(p), Status: "ok"}
	text, err := meta.ReadFile(real)
	switch {
	case errors.Is(err, meta.ErrNoMetadata):
		s.Status = "missing"
	case err != nil:
		s.Status, s.Error = "unreadable", err.Error()
	default:
		params := meta.Parse(text)
		s.Params = &params
	}
	attrs := []any{"path", p, "file", real, "status", s.Status}
	if s.Params != nil {
		attrs = append(attrs, "prompt_chars", len(s.Params.Prompt),
			"negative_chars", len(s.Params.Negative), "settings", len(s.Params.Settings))
	}
	if s.Error != "" {
		attrs = append(attrs, "err", s.Error)
	}
	a.log.Debug("read metadata", attrs...)
	return s, nil
}

// compare handles GET /api/compare?a=<file>&b=<file>. Either may be left
// out.
func (a *api) compare(w http.ResponseWriter, r *http.Request) {
	var resp compareResponse
	q := r.URL.Query()
	for _, x := range []struct {
		key string
		dst **side
	}{{"a", &resp.A}, {"b", &resp.B}} {
		if p := q.Get(x.key); p != "" {
			s, err := a.loadSide(p)
			if err != nil {
				a.log.Debug("compare refused", "side", x.key, "path", p, "err", err)
				writeError(w, err)
				return
			}
			*x.dst = s
		}
	}
	if resp.A != nil && resp.B != nil && resp.A.Params != nil && resp.B.Params != nil {
		pa, pb := resp.A.Params, resp.B.Params
		resp.Diff = &comparison{
			Prompt:   diff.Words(pa.Prompt, pb.Prompt),
			Negative: diff.Words(pa.Negative, pb.Negative),
			Settings: diff.Settings(pa.Settings, pb.Settings),
		}
		a.log.Debug("compared metadata", "a", resp.A.Path, "b", resp.B.Path,
			"prompt_segments", len(resp.Diff.Prompt), "settings_lines", len(resp.Diff.Settings))
	}
	writeJSON(w, http.StatusOK, resp)
}
