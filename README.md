# dview

## Description

dview is an image viewer for comparing generated images. It shows two images
side by side and lists the differences in their metadata below them. The goal
is to compare the prompts and settings used to generate the images and see
what differs between them.

dview runs as a small web server (port 8008 by default) and is used from a
web browser. Its interface is styled after Stable Diffusion WebUI Forge.

_Current status: the project is a skeleton that only prints `Hello, world!`._

## Requirements

The detailed requirements and open questions are in
[REQUIREMENTS.md](REQUIREMENTS.md).

To build, you need Go 1.27 or later and GNU Make.

## Usage

```bash
make build      # build the binary to bin/dview
make test       # run all tests
make clean      # remove build output
./bin/dview     # run the program
```

## Project layout

| Path           | Purpose                       |
| -------------- | ----------------------------- |
| `REQUIREMENTS.md` | Detailed requirements      |
| `main.go`      | Program entry point           |
| `main_test.go` | Unit tests                    |
| `Makefile`     | Build and test targets        |
| `go.mod`       | Go module definition (`dview`) |
