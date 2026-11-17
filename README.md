# dView

## Description

dView is an image viewer for comparing image metadata. It shows two images
and a diff of the image metadata.  Currently it only compares the `parameters`
field as used by A1111/Forge-style front-ends and only reads PNG, JPEG and
WebP image formats.

dView is a command-line binary and runs a small web server (http://localhost:8008
by default) to expose its user interface in a web browser. Its interface is
styled after Stable Diffusion WebUI Forge.

## Requirements

The detailed requirements and open questions are in
[REQUIREMENTS.md](REQUIREMENTS.md).

To build, you need Go 1.27 or later and GNU Make.

## Build

Some of the usual make targets (`build`, `test` and `clean`) are available:

```bash
make build      # build the binary to bin/dview
make test       # run all tests
make clean      # remove build output
```

`make test-browser` runs the web UI in a real browser and checks the file
dialog, the diff, the checkbox, the URL box and the Send buttons. `BROWSER`
picks the browser: `firefox` (the default), `chrome`, `safari`, or a
comma-separated list:

```bash
make test-browser BROWSER=firefox,chrome,safari
```

Firefox and Chrome run headless. Safari has no headless mode, so it runs
through Apple's `safaridriver` in a separate automation window that shows on
screen while the test runs. Safari must be allowed to be automated, which
requires an administrator password. From a shell:

```bash
safaridriver --enable
```

This can also be done within Safari by turning on **Develop > Allow Remote
Automation**.

The browser must be installed. If it isn't found on `PATH` or in the usual
macOS location, set `DVIEW_FIREFOX_PATH`, `DVIEW_CHROME_PATH` or
`DVIEW_SAFARIDRIVER_PATH` to it.

## Usage

```bash
./bin/dview [--config <file>] [--debug] [--listen [<address>:]<port>] [--url <URL>] [--dir [<name>=]<path>]... [--show-config]
```

| Option | Default | Meaning |
| ------ | ------- | ------- |
| `--debug` | off | Log major internal actions to the console: option parsing, every request, directory listings, metadata reads, and messages and errors sent back from the browser page. |
| `--listen [<address>:]<port>` | `localhost:8008` | Address and port to listen on. The address may be an IPv4 address, a bracketed IPv6 address such as `[::]`, or a hostname. A port alone keeps `localhost`. Any explicit address (not `0.0.0.0` or `[::]`) must be configured on an interface.|
| `--url <url>` | last used | Address of a Forge-like back end, for the **Send** buttons, for example `http://127.0.0.1:7860/`. It can also be edited at the top of the page, which remembers the last 4. |
| `--dir [<name>=]<path>` | current directory | Specify one or more directories to make available in the UI.  The optional `name` is used as the top-level directory name if multiple `--dir` options are given. The working directory is always allowed. Caution: A `<path>` of  `/` allows the whole filesystem. |
| `--config <file>` | (see below) | Read settings from this config file. See below. |
| `--show-config` | | Print the settings dView would use on stdout then exit. This can be redirected to a file to generate a `dview.yaml` file. |

Examples:

```bash
./bin/dView --listen 9000
./bin/dView --listen '[::]:8008' --dir /data/outputs --dir old=/archive/outputs
```

### Configuration File and Environment Variables

The `--listen`, `--url`, `--dir` and `--debug` options can also be set in a
YAML configuration file or an environment variable. The settings precedence
order is command line option, environment variable, configuration file and
lastly built-in defaults.

The YAML key names are the same as the option names, the environment variable
names are `DVIEW_` followed by the option name in upper case

The configuration file to ready can be specified by the `--config <file>`
option or the `DVIEW_CONFIG` environment variable (there is no `config` key
in the configuration file itself). Without the `--config` option dView searches
for a configuration file as follows, and reads the first one it finds:

1. `.dview.yaml` or `.dview.yml` in the current directory
2. `dview.yaml` or `dview.yml` in `$XDG_CONFIG_HOME`, if that environment
   variable is set
3. `dview.yaml` or `dview.yml` in `~/.config`

Only that one file is read. Unknown settings in it are reported as errors,
to catch typos:

```yaml
listen: '[::]:8008'
url: http://127.0.0.1:7860/
dir:
  - ~/forge/outputs
  - old=/archive/outputs
debug: false
```

This example represents the default values built in to the dView binary:

```yaml
listen: localhost:8008
url: ""
dir: []
debug: false
```

Note that `dir` can be a single path or aYAML array. Also, a leading `~`
in the path references the current user's home directory, similar to how
UNIX shells often work.  Note that `~username` is not substituted as the
named user's home directory.  Relative paths are relative to the current
directory dView is started in.

The environment variables are `DVIEW_LISTEN`, `DVIEW_URL`, `DVIEW_DIR` and
`DVIEW_DEBUG` (`true` or `false`). `DVIEW_DIR` takes several directories
separated by `:`, like `PATH`. An empty variable counts as not set.

`--show-config` prints the combined result as YAML. It's a quick way to see
what dView will use, or to start a config file. Which file the settings came
from isn't a setting, so `--config` and `DVIEW_CONFIG` don't appear in it:

```bash
dview --listen 0.0.0.0:8080 --show-config > ~/.config/dview.yaml
```

will produce the following in `~/.config/dview.yaml`:

```yaml
listen: 0.0.0.0:8080
url: ""
dir: []
debug: false
```

dView runs until you press Ctrl-C (or it gets SIGTERM).

Open `http://localhost:8008/` in a browser, use the **Open** button above
either image to pick files, and the metadata of the two images is compared
below them.

Multiple image files may be selected in the **Open** dialog, the first two
(as sorted alphanumerically) will be used if more than two are selected.
Selecting only a single file will show that image underneath the **Open**
button that was clicked.

**Send** copies that image's generation parameters to the clipboard and
brings up Forge (the URL at the top of the page). In Forge's txt2img tab,
paste them into the Prompt box and click ↙ (Read generation parameters) to
fill in all the settings.

**Copy Image** copies that image to the clipboard, like a browser's right-click
Copy Image, for pasting into Forge's img2img tab. JPEG and WebP images are
copied as PNG. Copying needs the page to be opened on `localhost` or over
https; otherwise use right-click Copy Image.

## Project layout

| Path | Purpose |
| ---- | ------- |
| `REQUIREMENTS.md` | Detailed requirements |
| `main.go` | Command-line handling (urfave/cli/v3) |
| `internal/options/` | Parsing and checking `--listen` and `--dir` |
| `internal/server/` | Web server, listeners and JSON API |
| `internal/server/web/` | Web UI (HTML, CSS, JavaScript), embedded in the binary |
| `internal/fsview/` | The file dialog's tree of allowed directories and access checks |
| `internal/meta/` | Reading and parsing A1111/Forge metadata |
| `internal/diff/` | Word diff for prompts, key-by-key diff for settings |
| `*_test.go` | Tests (stretchr/testify) |
| `internal/server/browser_test.go`, `internal/server/testdata/harness/` | Headless browser tests (`make test-browser`) |
| `Makefile` | Build and test targets |
| `go.mod` | Go module definition (`dView`) |

## Credits

dView was created from an interactive requirements session between
Dean Troyer <dtroyer@gmail.com> and Claude Opus 5.5 (model ID `claude-opus-5-5`),
the results of which are in [REQUIREMENTS.md](REQUIREMENTS.md). Claude write
the original code and documentation.

After rewriting some of the readme and requirements I plan to hand the README
and REQUIREMENTS files to a new Claude session and see if it can produce a
working implementation from scratch, and just how different it is.  That will
eventually appear as another branch in this repo.
