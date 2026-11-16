# dview

## Description

dview is an image viewer for comparing generated images. It shows two images
side by side and lists the differences in their metadata below them. The goal
is to compare the prompts and settings used to generate the images and see
what differs between them.

dview runs as a small web server (port 8008 by default) and is used from a
web browser. Its interface is styled after Stable Diffusion WebUI Forge.

## Requirements

The detailed requirements and open questions are in
[REQUIREMENTS.md](REQUIREMENTS.md).

To build, you need Go 1.27 or later and GNU Make.

## Usage

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
screen while the test runs. Safari must be allowed to be automated, once:

```bash
safaridriver --enable
```

That asks for an administrator password. Turning on **Develop > Allow Remote
Automation** in Safari does the same.

The browser must be installed. If it isn't found on `PATH` or in the usual
macOS location, set `DVIEW_FIREFOX_PATH`, `DVIEW_CHROME_PATH` or
`DVIEW_SAFARIDRIVER_PATH` to it.

```bash
./bin/dview [-x] [--listen [<address>:]<port>] [--url <URL>] [--dir [<name>=]<path>]...
```

| Option | Default | Meaning |
| ------ | ------- | ------- |
| `-x`, `--debug` | off | Log major internal actions to the console: option parsing, every request, directory listings, metadata reads, and messages and errors sent back from the browser page. |
| `--listen [<address>:]<port>` | `localhost:8008` | Address and port to listen on. The address may be an IPv4 address, a bracketed IPv6 address such as `[::]`, or a hostname. A port alone keeps `localhost`. |
| `--url <URL>` | last used | Address of a Forge-like back end, for the **Send** buttons, for example `http://127.0.0.1:7860/`. It can also be edited at the top of the page, which remembers the last 4. |
| `--dir [<name>=]<path>` | none | Also allow files under `<path>`, shown as `/<name>`. Can be repeated. The working directory is always allowed. An unnamed `--dir /` allows the whole filesystem. |

Examples:

```bash
./bin/dview --listen 9000
./bin/dview --listen '[::]:8008' --dir /data/outputs --dir old=/archive/outputs
```

dview runs until you press Ctrl-C (or it gets SIGTERM).

Open `http://localhost:8008/` in a browser, use the **Open** button above
either image to pick files, and the metadata of the two images is compared
below them.

**Send** copies that image's generation parameters to the clipboard and
brings up Forge (the URL at the top of the page). In Forge's txt2img tab,
paste them into the Prompt box and click ↙ (Read generation parameters) to
fill in all the settings.

**Copy Image** puts that image on the clipboard, like a browser's right-click
Copy Image, for pasting into Forge's img2img tab. JPEG and WebP images are
copied as PNG. Copying needs the page to be opened on `localhost` or over
https; otherwise use right-click Copy Image.

_Current status: the first version of the UI works: choosing images with the
built-in file dialog, showing them side by side, and the prompt and settings
diff. Metadata is read from PNG, JPEG and WebP files._

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
| `go.mod` | Go module definition (`dview`) |
