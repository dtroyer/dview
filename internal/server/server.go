// Package server runs dview's web server.
package server

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"time"

	"dview/internal/fsview"
	"dview/internal/options"
)

//go:embed web
var webFS embed.FS

// shutdownTimeout bounds how long Serve waits for open requests to finish.
const shutdownTimeout = 5 * time.Second

// Config is everything the server needs to run.
type Config struct {
	// Listen holds the addresses to listen on.
	Listen []netip.AddrPort
	// Roots holds the top-level directories files may be served from.
	Roots options.Roots
	// Log receives status messages, and debug messages when Debug is set. A
	// nil Log discards everything.
	Log *slog.Logger
	// Debug turns on logging of major internal actions, including requests
	// and messages forwarded from the browser.
	Debug bool
}

func (c Config) logger() *slog.Logger {
	if c.Log == nil {
		return slog.New(slog.DiscardHandler)
	}
	return c.Log
}

// Run opens a listener on every configured address and serves until ctx is
// cancelled.
func Run(ctx context.Context, cfg Config) error {
	log := cfg.logger()
	listeners, err := Listen(ctx, cfg.Listen)
	if err != nil {
		return err
	}
	for _, l := range listeners {
		log.Info("listening", "url", fmt.Sprintf("http://%s/", l.Addr()))
	}
	logRoots(log, cfg.Roots)
	err = Serve(ctx, listeners, Handler(cfg))
	log.Debug("server stopped", "err", err)
	return err
}

// network picks the socket type for an address. The IPv6 wildcard [::] uses
// "tcp" so it is dual-stack and also accepts IPv4 where the OS allows it.
// Every other address gets a socket of its own family only, so 0.0.0.0
// stays IPv4-only.
func network(a netip.Addr) string {
	switch {
	case a.Is6() && a.IsUnspecified():
		return "tcp"
	case a.Is4():
		return "tcp4"
	default:
		return "tcp6"
	}
}

// Listen opens a TCP listener on each address. If any of them fails, the
// ones already opened are closed and the error is returned.
func Listen(ctx context.Context, addrs []netip.AddrPort) ([]net.Listener, error) {
	if len(addrs) == 0 {
		return nil, errors.New("no addresses to listen on")
	}
	var lc net.ListenConfig
	listeners := make([]net.Listener, 0, len(addrs))
	for _, a := range addrs {
		l, err := lc.Listen(ctx, network(a.Addr()), a.String())
		if err != nil {
			for _, open := range listeners {
				open.Close()
			}
			return nil, fmt.Errorf("listening on %s: %w", a, err)
		}
		listeners = append(listeners, l)
	}
	return listeners, nil
}

// Serve serves handler on every listener until ctx is cancelled or one of
// them fails, then shuts down. It returns nil after a shutdown caused by
// ctx, and the first serving error otherwise.
func Serve(ctx context.Context, listeners []net.Listener, handler http.Handler) error {
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	errs := make(chan error, len(listeners))
	for _, l := range listeners {
		go func() { errs <- srv.Serve(l) }()
	}

	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-errs:
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && serveErr == nil {
		serveErr = fmt.Errorf("shutting down: %w", err)
	}
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}
	return serveErr
}

// Handler returns the HTTP handler for the web UI.
func Handler(cfg Config) http.Handler {
	static, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(err) // the embedded directory is fixed at build time
	}
	log := cfg.logger()
	a := &api{fs: fsview.New(cfg.Roots, log), log: log, debug: cfg.Debug}
	mux := http.NewServeMux()
	mux.Handle("GET /", noCache(http.FileServerFS(static)))
	mux.HandleFunc("GET /api/config", a.config)
	mux.HandleFunc("POST /api/log", a.clientLog)
	mux.HandleFunc("GET /api/list", a.list)
	mux.HandleFunc("GET /api/image", a.image)
	mux.HandleFunc("GET /api/compare", a.compare)
	if cfg.Debug {
		return logRequests(log, mux)
	}
	return mux
}

// noCache makes browsers check for a new copy of the page's files on every
// load. The files are built into the binary and have no modification time,
// so without this a browser could keep using files from an older build.
func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}

// statusRecorder captures the status code and size of a response.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += int64(n)
	return n, err
}

// logRequests logs every request at debug level after it is served.
func logRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		log.Debug("request",
			"method", r.Method,
			"uri", r.URL.RequestURI(),
			"status", rec.status,
			"bytes", rec.bytes,
			"duration", time.Since(start).Round(time.Microsecond),
			"remote", r.RemoteAddr,
			"agent", r.UserAgent())
	})
}

func logRoots(log *slog.Logger, roots options.Roots) {
	switch {
	case roots.WholeFS:
		log.Info("serving the whole filesystem", "path", "/")
	case roots.Virtual():
		for _, r := range roots.List {
			log.Info("serving", "as", "/"+r.Name, "path", r.Path)
		}
	default:
		for _, r := range roots.List {
			log.Info("serving", "path", r.Path)
		}
	}
}
