// Command dview is a web-based viewer that compares the generation metadata
// of two images side by side.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/urfave/cli/v3"

	"dview/internal/options"
	"dview/internal/server"
)

// deps holds what the command needs from the outside world, so tests can
// replace it.
type deps struct {
	getwd      func() (string, error)
	resolver   options.Resolver
	ifaceAddrs options.InterfaceAddrsFunc
	run        func(context.Context, server.Config) error
	stdout     io.Writer
	stderr     io.Writer
}

func defaultDeps() deps {
	return deps{
		getwd:      os.Getwd,
		resolver:   net.DefaultResolver,
		ifaceAddrs: options.LocalInterfaceAddrs,
		run:        server.Run,
		stdout:     os.Stdout,
		stderr:     os.Stderr,
	}
}

func newCommand(d deps) *cli.Command {
	return &cli.Command{
		Name:      "dview",
		Usage:     "compare the generation metadata of two images side by side",
		UsageText: "dview [-x] [--listen [<address>:]<port>] [--dir [<name>=]<path>]...",
		Writer:    d.stdout,
		ErrWriter: d.stderr,
		// Paths may contain commas, so --dir values must not be split.
		DisableSliceFlagSeparator: true,
		HideHelpCommand:           true,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "debug",
				Aliases: []string{"x"},
				Usage:   "log major internal actions, including browser errors, to the console",
			},
			&cli.StringFlag{
				Name:  "listen",
				Value: options.DefaultListen,
				Usage: "listen on `[<address>:]<port>`; the address may be an IPv4 address, " +
					"a bracketed IPv6 address or a hostname",
			},
			&cli.StringSliceFlag{
				Name: "dir",
				Usage: "also allow files under `[<name>=]<path>`; " +
					"can be given more than once",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Present() {
				return fmt.Errorf("unexpected argument %q; dview takes only options", cmd.Args().First())
			}
			debug := cmd.Bool("debug")
			log := newLogger(d.stderr, debug)
			log.Debug("debug logging on")

			spec, err := options.ParseListen(cmd.String("listen"))
			if err != nil {
				return err
			}
			log.Debug("parsed --listen", "value", cmd.String("listen"), "host", spec.Host, "port", spec.Port)
			addrs, err := options.ResolveListen(ctx, spec, d.resolver, d.ifaceAddrs)
			if err != nil {
				return err
			}
			log.Debug("resolved listen addresses", "addrs", addrs)

			cwd, err := d.getwd()
			if err != nil {
				return fmt.Errorf("finding the working directory: %w", err)
			}
			roots, err := options.ParseDirs(cwd, cmd.StringSlice("dir"))
			if err != nil {
				return err
			}
			for _, r := range roots.List {
				log.Debug("top-level directory", "name", r.Name, "path", r.Path)
			}
			log.Debug("file tree", "virtual_root", roots.Virtual(), "whole_filesystem", roots.WholeFS)

			return d.run(ctx, server.Config{Listen: addrs, Roots: roots, Log: log, Debug: debug})
		},
	}
}

// newLogger returns a text logger on w that includes debug messages when
// debug is set.
func newLogger(w io.Writer, debug bool) *slog.Logger {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	if debug {
		opts.Level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(w, opts))
}

func main() {
	d := defaultDeps()
	// Ctrl-C or SIGTERM cancels the context, which shuts the server down.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := newCommand(d).Run(ctx, os.Args); err != nil {
		stop()
		fmt.Fprintln(d.stderr, "dview:", err)
		os.Exit(1)
	}
}
