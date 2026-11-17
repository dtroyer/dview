# dView Requirements

This file lists the specific requirements for dview as they are identified.
The project overview is in [README.md](README.md).

## Functional

1. Show two images side by side.
2. Read the metadata embedded in each image, including the generation prompt
   and settings.
3. Show the differences in metadata between the two images below them.
4. Make the differences in the generation prompts easy to see.

## Choosing images

1. dView operates on image that are on the system where it is running, not on
   the system where the browser is running (when they are not the same system).
2. The file open dialog is built into the dView web UI so it can show images
   on the system where it is running; the browser's own file dialog is not used
   as it can only pick files on the browser's local filesystem.
3. The file dialog:
   - Is styled to match the rest of the UI.
   - Is opened by an Open button above each image (see User interface).
   - The first time each button is used, starts at the top of the tree: the
     virtual root, or the working directory when there is no virtual root.
   - Each Open button remembers the directory its dialog was last in,
     independently of the other button, including after the dialog is
     cancelled and after the page is reloaded.
   - With no virtual root, shows paths relative to the working directory.
   - Lets you move into subdirectories and back up to parent directories,
     but not above a top-level directory (see below).
   - Allows more than one file to be selected.
   - Lists only directories and image files. Image files are those with the
     extensions of the supported formats: `.png`, `.jpg`, `.jpeg` and
     `.webp`, in any letter case.
   - Lists entries in this order:
     1. `..`, but only when moving up is allowed (that is, not at the top
        of the tree: the virtual root, the working directory when there is
        no virtual root, or `/` when the whole filesystem is allowed).
     2. Directories.
     3. Image files.
   - Sorts the directories and the image files alphanumerically by name, in
     plain character order (so `img10.png` comes before `img2.png`, and
     uppercase before lowercase). Natural, macOS Finder-style sorting may be
     added later.
   - Doesn't list hidden entries (names starting with `.`), other than `..`.
4. Both Open buttons behave the same, except for where the chosen files go:
   - The first selected file becomes the image under the button that was
     pressed.
   - A second selected file, if any, becomes the other image.
   - If more than two files are selected, only the first two are used, in
     the order the dialog lists them.
   - If only one file is selected, only the pressed button's image changes.
     (This replaces the earlier rule of opening the dialog a second time:
     the other button picks the other image, from any directory.)
5. In the dialog, clicking a directory opens it; clicking a file selects or
   deselects it. Double-clicking a file opens just that file. Enter opens the
   selection and Escape cancels.
6. Access is limited to a set of top-level directories:
   - The directory dView was started from (its working directory) is always
     one of them.
   - Each `--dir [<name>=]<path>` option adds another. `--dir` can be given
     more than once. A relative `<path>` is relative to the working
     directory. A leading `~` character in the path makes the path relative
     to the current user's home directory (remember this is the user running
     dView, not the browser user).
   - `<name>` is optional and sets the directory's name in the virtual root
     (see below). Without it, the name is the directory's base name. The
     name is the text before the first `=`, so a path that contains `=` must
     be given with a name.
   - A name can't be empty or contain `/`.
   - Everything below a top-level directory is available. Nothing above
     them is.
   - dView exits with an error message if a `--dir` path doesn't exist or
     isn't a directory, if a name is invalid, or if two top-level
     directories end up with the same name. The duplicate-name message
     suggests the `name=path` form.
7. The server enforces the limit on every request, not just in the dialog.
   It resolves `..` and symbolic links first, then rejects any path that
   ends up outside the top-level directories, so a crafted request can't
   reach files outside them.
8. Symbolic links are followed only if their target is inside one of the
   top-level directories (any of them, not only the one that holds the
   link). Links whose target is outside all of them, or doesn't exist, are
   not followed and are not listed in the file dialog.
9. When there is more than one top-level directory, the file dialog shows
   them in a virtual root, `/`:
   - Each top-level directory appears in the virtual root as `/<name>`.
     `<name>` is the name given with `--dir name=path`, or else the
     directory's base name like `$(basename $PWD)` in a shell. For example,
     `dview --dir /data/outputs --dir old=/archive/outputs` started in
     `/home/me/work` shows `/outputs`, `/old` and `/work`.
   - They are listed with the `--dir` directories first, in the order given
     on the command line, then the working directory.
   - Paths are shown from the virtual root, for example the options above might
     produce a filename of `/old/2026-11-16/00042-123456.png`.
   - The virtual root is the highest level; you can't go above it.
   - With only one top-level directory (no `--dir`), there is no virtual
     root, and paths are shown relative to the working directory.
10. If `/` is given as a `--dir` without a name (`--dir /`), the whole
    filesystem is allowed. There is no virtual root: the file dialog shows
    the real filesystem from `/`, and paths are shown as real absolute
    paths. The other top-level directories, including the working
    directory, are inside `/` and are reached through it. If `/` is given
    with a name (`--dir all=/`), it is an ordinary entry in the virtual
    root, like any other top-level directory.
11. The server reads the selected files from disk and parses their metadata.
    The browser asks the server for the image and the parsed metadata.
12. Excluding paths from the allowed directories (for example, disallowing
    `/etc` when `/` is allowed) may be added later. It is not an initial
    requirement.

## Metadata formats

1. Support the generation metadata written by AUTOMATIC1111 (A1111) and Forge.
   Other formats, such as ComfyUI, may be added later.
2. Read the metadata from where A1111/Forge store it:
   - PNG: the `parameters` text chunk (`tEXt` or `iTXt`).
   - JPEG and WebP: the EXIF `UserComment` field.
3. Parse the `parameters` text into separate fields:
   - **Prompt:** everything before the `Negative prompt:` line or the settings
     line. It can span several lines.
   - **Negative prompt:** the text after `Negative prompt:`, up to the
     settings line. It can span several lines and may be missing.
   - **Settings:** the last line, consisting of comma-separated `Key: value` pairs
     (for example `Steps: 10, Sampler: DPM++ 2S a, CFG scale: 6` and so on). Values may be quoted and may contain commas inside the quotes.
4. If an image has no metadata, or the metadata can't be parsed, show the
   image anyway and say that its metadata is missing or unreadable.

## Metadata comparison

Image 1 is the left image and image 2 is the right image. Each image's
metadata is shown in the column below it. Image 1 is the base, and image 2 is
compared against it.

1. **Prompt:**
   - Under image 1, the metadata is shown as-is.
   - Under image 2, the metadata is shown as an inline text diff against
     image 1's metadata. Text that image 2 adds is shown in green. Text that
     image 2 removes (present in image 1 but not image 2) is shown in red,
     without strikethrough.
     Unchanged text is shown normally.
   - The diff compares words, which works for both descriptive sentences and
     comma-separated lists of attributes.
   - Punctuation such as `,` `(` `)` `:` and `.` counts as separate tokens
     from the words next to it. So `girl,` against `girl` shows only the
     comma as changed, and in a weighted term such as `(red hair:1.2)` a
     change to the weight shows only the number as changed.
   - Whitespace and line breaks are kept as written, but differences that
     are only in whitespace are not shown as changes.
2. **Negative prompt:** handled the same way as the prompt.
3. **Settings** (the `Steps: …` line):
   - Each `Key: value` pair is shown on its own line.
   - Under image 1, the settings are shown as-is, one per line.
   - Under image 2, the settings are shown as a unified diff against
     image 1's settings, with each line prefixed and colored:
     - `+` in green: a key only in image 2, or the new value of a changed
       key.
     - `-` in red: a key only in image 1, or the old value of a changed key.
     - ` ` (a space) with no color: a key with the same value in both.
   - The order of the keys is ignored when comparing. Pairs are matched by
     key, so the same settings in a different order show no differences.
   - Lines are listed in image 1's key order. Keys that are only in image 2
     follow, in image 2's order. For a changed key, the `-` line comes
     before the `+` line.
4. If only one image is shown, its metadata is shown as-is, with no diff.
5. If either image's metadata is missing or unreadable, the other image's
   metadata is shown as-is, with no diff.
6. A "Show only changed lines" checkbox under image 2's metadata panel
   filters image 2's diff:
   - Settings: lines with the same value in both images are hidden.
   - Prompt and negative prompt: only lines that contain a change are
     shown, with `⋯` where unchanged lines were left out.
   - A section with no changes says "No changes."
   - The checkbox is checked by default. It is disabled when there is no
     diff to filter. Its setting is remembered across page reloads.

## User interface

1. The UI is a web page served by the dView binary. Nothing else needs to be
   installed or run.
2. The UI has the look and feel of the Stable Diffusion WebUI Forge
   interface (a Gradio-style layout): a dark theme by default, an orange
   accent color for primary actions and highlights, content grouped in
   rounded panels, and compact labeled fields.
3. The page layout, from top to bottom:
   - A header with space for a logo in the top-left corner. Until there is a
     logo, it shows "dView" in an Optima-like typeface. On the right of the
     header, the Forge URL box (see Sending to a back end).
   - Open, Send and Copy Image buttons above each image, with the selected
     file's path shown under them.
   - The two images side by side.
   - Below each image, that image's metadata (see Metadata comparison).
   - Under image 2's metadata, the "Show only changed lines" checkbox.
4. The page fits the window without scrolling:
   - The images are scaled to fit the window, keeping their aspect ratio,
     with at least 2em of space around each image.
   - Each image's metadata is in its own panel, which scrolls when the text
     doesn't fit.
5. On narrow screens (phones), the space around the images is reduced so
   they stay usable.

## Sending to a back end

Either image's generation metadata can be sent to a Forge-like back end's
txt2img tab, to be worked on further in another browser window.

1. Stock Forge has no way for another program to fill in the txt2img form
   in a browser tab that is already open. So sending uses Forge's own
   "Read generation parameters" button (↙ next to the Prompt box):
   - Send copies the image's metadata text to the clipboard, exactly as it
     is stored in the image (A1111 format).
   - Send then brings up the Forge tab that dView opened before, without
     reloading it, so nothing in it is lost. If there is none, it opens the
     Forge URL in a new tab.
   - The user pastes the text into the txt2img Prompt box and clicks ↙,
     which fills in the prompt, negative prompt and settings.
   - A message on the page says what was copied and what to do next. If the
     browser blocks opening the tab, the message says to switch to Forge
     by hand.
2. The `--url <URL>` option gives the back end's address. It must be an
   `http://` or `https://` URL with a host, or dView exits with an error.
   Without `--url`, the page starts with the last URL used in that browser.
3. The header shows the URL in a text box that can be edited. A ▾ button
   next to it opens a menu of the last 4 URLs used, most recent first.
   - A URL is added to the menu when it is finished being edited (Enter or
     leaving the box), picked from the menu, used by Send, or given with
     `--url`.
   - A box that doesn't hold a valid URL is outlined in red, and invalid
     URLs are not added to the menu.
   - The menu is kept in the browser, like the remembered directories.
4. Each image has a Send button next to its Open button. It is disabled
   when that image has no generation metadata.
5. Copying uses the browser's Clipboard API, and falls back to the older
   copy command when the API isn't available or is refused (for example,
   when dView is reached over plain http on a network address).
6. Each image also has a Copy Image button, which puts the image itself on
   the clipboard, like a browser's right-click Copy Image, for pasting into
   Forge's img2img tab:
   - It is enabled whenever an image is shown, with or without metadata.
   - The image is copied as PNG, the one image type every browser accepts
     on the clipboard; JPEG and WebP images are converted. The clipboard
     copy carries only the picture, not the generation metadata (Send
     does that).
   - Browsers only allow copying images from pages opened on `localhost`
     or over https. Elsewhere, and if the copy is refused, a message
     suggests right-click Copy Image instead.
   - A message after copying says to paste it into img2img's image area.

## Web server

1. The dView binary is the web server.
2. By default it listens on `localhost` port `8008`, so only this machine can
   connect.
3. The `--listen` option sets the listen address and port. Its argument has
   the form `[<address>:]<port>`:
   - `<address>` is optional. It can be an IPv4 address, an IPv6 address, or
     a hostname. An IPv6 address is written in square brackets, as in `[::]`
     or `[::1]`, so its colons aren't mistaken for the port separator.
   - `<port>` is an integer and is required.
   - If only `<port>` is given, only the port changes and the address stays
     `localhost`.
4. Examples:
   - `--listen 9000` listens on `localhost:9000`.
   - `--listen 0.0.0.0:8008` listens on all IPv4 interfaces.
   - `--listen [::]:8008` listens on all interfaces (IPv6, and IPv4 where the
     OS allows dual-stack).
   - `--listen 127.0.0.1:8080` listens on the IPv4 loopback address.
5. A hostname is resolved with a DNS lookup when dView starts. dView listens
   on every resolved address that is on a local network interface.
   Exception: `localhost` (including the default) listens on `[::1]` and
   `127.0.0.1`. Both are needed: tested on 2026-11-16 on Linux and macOS, a
   socket on `[::1]` alone refuses IPv4 connections to `127.0.0.1`. Only the
   `[::]` wildcard accepts both IPv4 and IPv6.
6. dView checks the address before it starts listening. It exits with an
   error message if:
   - the `--listen` value doesn't match the format (the message shows the
     expected format),
   - the port isn't a valid port number,
   - the hostname lookup fails, or
   - the address isn't on a local network interface, or none of the
     addresses the hostname resolves to are. The wildcard addresses `0.0.0.0` and `[::]`
     are always allowed. Resolved addresses that aren't on a local interface
     are skipped.
7. All web assets (HTML, CSS, JavaScript) are embedded in the binary.
   Browsers are told to check for a new copy on every load, so a new build
   is never hidden by a cached old one.

## Logging

1. dView logs with the Go standard library's structured logger
   (`log/slog`), as text, to standard error.
2. By default it logs only the addresses it listens on and the directories
   it serves, plus errors.
3. The `--debug` option also logs major internal actions:
   - the settings and the config file they came from, the parsed options,
     resolved listen addresses and top-level directories,
   - every HTTP request, with its status, size and duration,
   - directory listings, skipped symbolic links, image requests, metadata
     reads and comparisons, and refused paths,
   - messages and errors from the web page: page load, Open button clicks,
     the file dialog opening, listing and closing, and any JavaScript error
     or failed resource. The page sends these to the server only when
     `--debug` is on.

## Configuration

1. The settings `listen`, `url`, `dir` and `debug` correspond to the
   options `--listen`, `--url`, `--dir` and `--debug`. Each can come from:
   - the command line (highest priority),
   - an environment variable, `DVIEW_` plus the option name in uppercase:
     `DVIEW_LISTEN`, `DVIEW_URL`, `DVIEW_DIR`, `DVIEW_DEBUG`,
   - the config file (lowest priority),
   - otherwise, the built-in default.
2. A setting from a higher-priority source replaces the lower one entirely.
   For `dir`, any `--dir` on the command line replaces the directories from
   the environment and the config file; they are not combined.
3. The config file is YAML, read once at startup. Only one file is read:
   - `--config <file>` names it. Otherwise the `DVIEW_CONFIG` environment
     variable does. A relative name is relative to the directory dView is
     started in. A named file must exist, or dview exits with an error.
   - When no file is named, dView reads the first of these that exists:
     1. `./.dview.yaml`, then `./.dview.yml`, in the dView working directory,
     2. `$XDG_CONFIG_HOME/dview.yaml`, then `dview.yml`, when
        `XDG_CONFIG_HOME` is set,
     3. `$HOME/.config/dview.yaml`, then `$HOME/.config/dview.yml`.
   - When none of them exists, the defaults are used; that is not an error.
   - Unknown settings, and invalid YAML, are errors that stop dView, so
     typos aren't silently ignored.
   - `dir` is a list of `[<name>=]<path>` values, or a single one.
4. Environment variables:
   - `DVIEW_DIR` holds one or more `--dir` values separated by `:`, like
     `PATH`.
   - `DVIEW_DEBUG` is `true` or `false` (or `1`/`0`); anything else is an
     error.
   - An empty variable counts as not set.
5. In the config file and environment variables, a path starting with `~`
   means the home directory, as the shell does on the command line.
   Relative paths are relative to the dView working directory.
6. `--show-config` prints the settings dView would use, combined from all
   sources and including defaults, as the contents of a `dview.yaml`, to
   standard output, then exits without starting the server and with no
   other output. Its output can be saved as the config file.
7. `--show-config` is only a command-line option. It can't be set in the
   config file (it's reported as an unknown setting), and
   `DVIEW_SHOW_CONFIG` is ignored.
8. `--config` and `DVIEW_CONFIG` choose the file; they are not settings.
   They don't appear in `--show-config`'s output, and `config` in the
   config file is reported as an unknown setting.
9. The config file's settings are top-level keys: no `dview:` key around
   them and no `version` key. Backward compatibility is required: any
   config file that works with one release must keep working, with the
   same meaning, in later releases. New settings may be added; existing
   ones are not renamed, removed or changed in meaning.

## Platform

1. The target platforms are macOS and Linux, with IPv4 and IPv6 dual-stack
   networking. Other platforms may be supported later.

## Build and development

1. Written in Go 1.27 or later.
2. Build and test with GNU Make (`make build`, `make test`).
3. Command-line options are handled with `github.com/urfave/cli/v3`.
4. Tests use `github.com/stretchr/testify`.
5. `make test-browser` runs the web UI in a real browser:
   - `BROWSER` selects the browser: `firefox` (default), `chrome` (Chrome
     or Chromium), `safari` (macOS only), or a comma-separated list.
   - Firefox and Chrome run headless. Safari can't, so it runs through
     Apple's `safaridriver` (WebDriver) in a separate automation window
     with its own isolated storage. It needs remote automation allowed once
     (`safaridriver --enable`), which the test's error message explains.
   - The tests drive the UI the way a user would: the file dialog, choosing
     images, the diff, the changed-lines checkbox, the URL box and the Send
     buttons' enabled state. They also fail on any JavaScript error.
   - They need no tools beyond Go and the installed browser (and, for
     Safari, the `safaridriver` that ships with macOS). Each run uses a
     fresh, temporary browser profile.
   - What they can't cover: the clipboard copies (Send and Copy Image) and
     opening the Forge tab need a real user's click, which a script can't
     fake. The tests do check Copy Image's JPEG-to-PNG conversion.
   - Plain `make test` doesn't run them.
