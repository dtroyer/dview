// Package server runs dview's web server.
package server

import (
	"context"
	"fmt"
	"io"
	"net/netip"

	"dview/internal/options"
)

// Config is everything the server needs to run.
type Config struct {
	// Listen holds the addresses to listen on.
	Listen []netip.AddrPort
	// Roots holds the top-level directories files may be served from.
	Roots options.Roots
	// Log receives status messages.
	Log io.Writer
}

// Run starts the listeners and serves until ctx is cancelled.
//
// STUB: the listeners are not implemented yet. Run reports what it would
// listen on and serve, then returns.
func Run(ctx context.Context, cfg Config) error {
	for _, addr := range cfg.Listen {
		fmt.Fprintf(cfg.Log, "would listen on %s (not implemented yet)\n", addr)
	}
	switch {
	case cfg.Roots.WholeFS:
		fmt.Fprintln(cfg.Log, "serving the whole filesystem from /")
	case cfg.Roots.Virtual():
		for _, r := range cfg.Roots.List {
			fmt.Fprintf(cfg.Log, "serving /%s from %s\n", r.Name, r.Path)
		}
	default:
		for _, r := range cfg.Roots.List {
			fmt.Fprintf(cfg.Log, "serving %s\n", r.Path)
		}
	}
	return nil
}
