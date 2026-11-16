package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path"

	"dview/internal/diff"
	"dview/internal/fsview"
	"dview/internal/meta"
)

// api serves the JSON and image endpoints used by the web UI.
type api struct {
	fs *fsview.FS
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
	l, err := a.fs.List(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}

// image handles GET /api/image?path=<file>.
func (a *api) image(w http.ResponseWriter, r *http.Request) {
	real, err := a.fs.OpenImage(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, err)
		return
	}
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
	}
	writeJSON(w, http.StatusOK, resp)
}
