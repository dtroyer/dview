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

```bash
./bin/dview [--listen [<address>:]<port>] [--dir [<name>=]<path>]...
```

| Option | Default | Meaning |
| ------ | ------- | ------- |
| `--listen [<address>:]<port>` | `localhost:8008` | Address and port to listen on. The address may be an IPv4 address, a bracketed IPv6 address such as `[::]`, or a hostname. A port alone keeps `localhost`. |
| `--dir [<name>=]<path>` | none | Also allow files under `<path>`, shown as `/<name>`. Can be repeated. The working directory is always allowed. An unnamed `--dir /` allows the whole filesystem. |

Examples:

```bash
./bin/dview --listen 9000
./bin/dview --listen '[::]:8008' --dir /data/outputs --dir old=/archive/outputs
```

_Current status: the options are parsed and checked, but the web server is
a stub. dview prints the addresses it would listen on and the directories it
would serve, then exits._

## Project layout

| Path | Purpose |
| ---- | ------- |
| `REQUIREMENTS.md` | Detailed requirements |
| `main.go` | Command-line handling (urfave/cli/v3) |
| `internal/options/` | Parsing and checking `--listen` and `--dir` |
| `internal/server/` | Web server (stub for now) |
| `*_test.go` | Tests (stretchr/testify) |
| `Makefile` | Build and test targets |
| `go.mod` | Go module definition (`dview`) |
