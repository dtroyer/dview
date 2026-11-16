// Package server runs dview's web server.
package server

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
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
	// Log receives status messages.
	Log io.Writer
}

// Run opens a listener on every configured address and serves until ctx is
// cancelled.
func Run(ctx context.Context, cfg Config) error {
	listeners, err := Listen(ctx, cfg.Listen)
	if err != nil {
		return err
	}
	for _, l := range listeners {
		fmt.Fprintf(cfg.Log, "listening on http://%s/\n", l.Addr())
	}
	logRoots(cfg.Log, cfg.Roots)
	return Serve(ctx, listeners, Handler(cfg))
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
	a := &api{fs: fsview.New(cfg.Roots)}
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(static))
	mux.HandleFunc("GET /api/list", a.list)
	mux.HandleFunc("GET /api/image", a.image)
	mux.HandleFunc("GET /api/compare", a.compare)
	return mux
}

func logRoots(w io.Writer, roots options.Roots) {
	switch {
	case roots.WholeFS:
		fmt.Fprintln(w, "serving the whole filesystem from /")
	case roots.Virtual():
		for _, r := range roots.List {
			fmt.Fprintf(w, "serving /%s from %s\n", r.Name, r.Path)
		}
	default:
		for _, r := range roots.List {
			fmt.Fprintf(w, "serving %s\n", r.Path)
		}
	}
}
