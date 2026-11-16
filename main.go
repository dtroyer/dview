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
	lookupEnv  func(string) (string, bool)
	resolver   options.Resolver
	ifaceAddrs options.InterfaceAddrsFunc
	run        func(context.Context, server.Config) error
	stdout     io.Writer
	stderr     io.Writer
}

func defaultDeps() deps {
	return deps{
		getwd:      os.Getwd,
		lookupEnv:  os.LookupEnv,
		resolver:   net.DefaultResolver,
		ifaceAddrs: options.LocalInterfaceAddrs,
		run:        server.Run,
		stdout:     os.Stdout,
		stderr:     os.Stderr,
	}
}

func newCommand(d deps) *cli.Command {
	return &cli.Command{
		Name:  "dview",
		Usage: "compare the generation metadata of two images side by side",
		UsageText: "dview [--config <file>] [--debug] [--listen [<address>:]<port>] [--url <URL>] " +
			"[--dir [<name>=]<path>]... [--show-config]",
		Description: "Settings are read, from lowest to highest priority, from a config file, " +
			"the environment variables DVIEW_LISTEN, DVIEW_URL, DVIEW_DIR (several separated " +
			"by ':') and DVIEW_DEBUG, and the command line.\n\n" +
			"The config file is the one named by --config or DVIEW_CONFIG. Otherwise it is the " +
			"first found of ./.dview.yaml, ./.dview.yml, then dview.yaml or dview.yml in " +
			"$XDG_CONFIG_HOME (when set) and in $HOME/.config.",
		Writer:    d.stdout,
		ErrWriter: d.stderr,
		// Paths may contain commas, so --dir values must not be split.
		DisableSliceFlagSeparator: true,
		HideHelpCommand:           true,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "config",
				Usage: "read settings from `file` instead of searching for a config file",
			},
			&cli.BoolFlag{
				Name:  "debug",
				Usage: "log major internal actions, including browser errors, to the console",
			},
			&cli.StringFlag{
				Name: "listen",
				Usage: "listen on `[<address>:]<port>`; the address may be an IPv4 address, " +
					"a bracketed IPv6 address or a hostname (default: " + options.DefaultListen + ")",
			},
			&cli.StringFlag{
				Name: "url",
				Usage: "the Forge-style back end's `URL`, shown at the top of the page and used " +
					"by the Send buttons, for example http://127.0.0.1:7860/",
			},
			&cli.StringSliceFlag{
				Name: "dir",
				Usage: "also allow files under `[<name>=]<path>`; " +
					"can be given more than once",
			},
			&cli.BoolFlag{
				Name:  "show-config",
				Usage: "print the settings dview would use, as the contents of dview.yaml, and exit",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Present() {
				return fmt.Errorf("unexpected argument %q; dview takes only options", cmd.Args().First())
			}
			cwd, err := d.getwd()
			if err != nil {
				return fmt.Errorf("finding the working directory: %w", err)
			}
			settings, configPath, err := loadSettings(cmd, d.lookupEnv, cwd)
			if err != nil {
				return err
			}
			if cmd.Bool("show-config") {
				out, err := settings.Marshal()
				if err != nil {
					return err
				}
				_, err = d.stdout.Write(out)
				return err
			}

			debug := settings.Debug
			log := newLogger(d.stderr, debug)
			log.Debug("debug logging on")
			log.Debug("settings", "config_file", configPath, "listen", settings.Listen,
				"url", settings.URL, "dir", []string(settings.Dir))

			spec, err := options.ParseListen(settings.Listen)
			if err != nil {
				return err
			}
			log.Debug("parsed --listen", "value", settings.Listen, "host", spec.Host, "port", spec.Port)
			addrs, err := options.ResolveListen(ctx, spec, d.resolver, d.ifaceAddrs)
			if err != nil {
				return err
			}
			log.Debug("resolved listen addresses", "addrs", addrs)

			roots, err := options.ParseDirs(cwd, settings.Dir)
			if err != nil {
				return err
			}
			for _, r := range roots.List {
				log.Debug("top-level directory", "name", r.Name, "path", r.Path)
			}
			log.Debug("file tree", "virtual_root", roots.Virtual(), "whole_filesystem", roots.WholeFS)

			backendURL, err := options.ParseBackendURL(settings.URL)
			if err != nil {
				return err
			}
			log.Debug("back end", "url", backendURL)

			return d.run(ctx, server.Config{
				Listen: addrs, Roots: roots, BackendURL: backendURL, Log: log, Debug: debug,
			})
		},
	}
}

// loadSettings merges, from lowest to highest priority, the built-in
// defaults, the config file, the DVIEW_* environment variables and the
// options given on the command line. It returns the config file's path, or
// "" when there is none.
//
// The config file is the one named by --config, else by DVIEW_CONFIG, else
// the first found in the usual places. The file's own location is not a
// setting, so it never appears in --show-config's output.
func loadSettings(cmd *cli.Command, lookupEnv func(string) (string, bool), cwd string) (options.Settings, string, error) {
	s := options.DefaultSettings()
	named := ""
	if v, ok := lookupEnv(options.EnvConfig); ok {
		named = v
	}
	if cmd.IsSet("config") {
		named = cmd.String("config")
		if named == "" {
			return s, "", fmt.Errorf("--config needs a file name")
		}
	}
	path, err := options.FindConfigFile(named, cwd, lookupEnv)
	if err != nil {
		return s, "", err
	}
	if path != "" {
		if err := options.LoadConfigFile(path, &s); err != nil {
			return s, path, err
		}
	}
	if err := options.ApplyEnv(&s, lookupEnv); err != nil {
		return s, path, err
	}
	if cmd.IsSet("listen") {
		s.Listen = cmd.String("listen")
	}
	if cmd.IsSet("url") {
		s.URL = cmd.String("url")
	}
	if cmd.IsSet("dir") {
		s.Dir = cmd.StringSlice("dir")
	}
	if cmd.IsSet("debug") {
		s.Debug = cmd.Bool("debug")
	}
	return s, path, nil
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
