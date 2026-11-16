//go:build browser && unix

// Browser tests: run the web UI in a real browser.
//
//	make test-browser                      # Firefox
//	make test-browser BROWSER=chrome       # Chrome or Chromium
//	make test-browser BROWSER=safari       # Safari (macOS)
//	make test-browser BROWSER=firefox,chrome,safari
//
// The test serves dview's handler with generated sample images, plus a test
// page (testdata/harness) that loads the UI in a same-origin frame, drives
// it the way a user would, and posts the result of each step back here.
//
// Firefox and Chrome run headless with a fresh, temporary profile. Safari
// has no headless mode: it is driven through Apple's safaridriver
// (WebDriver), which opens a separate automation window with its own
// isolated storage. safaridriver needs remote automation allowed once:
// run "safaridriver --enable", or turn on Develop > Allow Remote Automation
// in Safari.
//
// DVIEW_TEST_BROWSER picks the browsers (comma-separated). A browser that
// isn't in a usual place can be given with DVIEW_FIREFOX_PATH,
// DVIEW_CHROME_PATH or DVIEW_SAFARIDRIVER_PATH.
package server

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"dview/internal/options"
)

//go:embed testdata/harness
var harnessFS embed.FS

// browserTimeout bounds a whole run in one browser.
const browserTimeout = 90 * time.Second

// testBackendURL is passed as --url, and the test page checks it is shown.
const testBackendURL = "http://127.0.0.1:7860/"

// stepResult is what the test page posts after each step.
type stepResult struct {
	Step   string `json:"step"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	Done   bool   `json:"done"`
}

func TestBrowser(t *testing.T) {
	names := os.Getenv("DVIEW_TEST_BROWSER")
	if names == "" {
		names = "firefox"
	}
	for _, name := range strings.Split(names, ",") {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		t.Run(name, func(t *testing.T) { runBrowser(t, name) })
	}
}

// browser describes how to find and start one browser.
type browser struct {
	env      string   // environment variable with an explicit path
	commands []string // names to look up on PATH
	macPaths []string // full paths on macOS
	macOnly  bool
	// start launches the browser on url and returns once it is loading.
	start func(t *testing.T, bin, url string) *running
}

// running is a started browser.
type running struct {
	exited <-chan error  // receives when the browser (or its driver) exits
	stop   func()        // stops it and waits for it to exit
	output func() string // what it printed, for failure messages
}

var browsers = map[string]browser{
	"firefox": {
		env:      "DVIEW_FIREFOX_PATH",
		commands: []string{"firefox", "firefox-esr"},
		macPaths: []string{"/Applications/Firefox.app/Contents/MacOS/firefox"},
		start: func(t *testing.T, bin, url string) *running {
			profile := t.TempDir()
			prefs := strings.Join([]string{
				`user_pref("browser.shell.checkDefaultBrowser", false);`,
				`user_pref("browser.aboutwelcome.enabled", false);`,
				`user_pref("datareporting.policy.dataSubmissionEnabled", false);`,
				`user_pref("toolkit.telemetry.reportingpolicy.firstRun", false);`,
				`user_pref("app.update.disabledForTesting", true);`,
			}, "\n") + "\n"
			require.NoError(t, os.WriteFile(filepath.Join(profile, "user.js"), []byte(prefs), 0o644))
			return startProcess(t, bin, "-headless", "-no-remote", "-profile", profile, url)
		},
	},
	"chrome": {
		env:      "DVIEW_CHROME_PATH",
		commands: []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome"},
		macPaths: []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		},
		start: func(t *testing.T, bin, url string) *running {
			return startProcess(t, bin,
				"--headless=new", "--no-first-run", "--no-default-browser-check",
				"--disable-gpu", "--disable-extensions", "--user-data-dir="+t.TempDir(), url)
		},
	},
	"safari": {
		env:      "DVIEW_SAFARIDRIVER_PATH",
		commands: []string{"safaridriver"},
		macPaths: []string{"/usr/bin/safaridriver"},
		macOnly:  true,
		start:    startSafari,
	},
}

func init() { browsers["chromium"] = browsers["chrome"] }

func (b browser) find() (string, error) {
	if b.macOnly && runtime.GOOS != "darwin" {
		return "", fmt.Errorf("only available on macOS")
	}
	if p := os.Getenv(b.env); p != "" {
		return p, nil
	}
	for _, c := range b.commands {
		if p, err := exec.LookPath(c); err == nil {
			return p, nil
		}
	}
	if runtime.GOOS == "darwin" {
		for _, p := range b.macPaths {
			if _, err := os.Stat(p); err == nil {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("not found on PATH or in the usual places; set %s", b.env)
}

// lockedBuffer collects a process's output safely while it runs.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// startProcess runs bin in its own process group, so it and all its helper
// processes can be stopped together.
func startProcess(t *testing.T, bin string, args ...string) *running {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out := &lockedBuffer{}
	cmd.Stdout, cmd.Stderr = out, out
	require.NoError(t, cmd.Start())
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	var once sync.Once
	return &running{
		exited: exited,
		output: out.String,
		stop: func() {
			once.Do(func() {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				select {
				case <-exited:
				case <-time.After(10 * time.Second):
					t.Logf("%s did not exit after being killed", filepath.Base(bin))
				}
			})
		},
	}
}

// ---- Safari, through safaridriver (WebDriver) --------------------------------

// webDriver calls a WebDriver endpoint and returns the response's "value".
func webDriver(method, url string, body any) (json.RawMessage, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Value json.RawMessage `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("%s %s: HTTP %d, unreadable reply: %w", method, url, resp.StatusCode, err)
	}
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(out.Value, &e)
		return nil, fmt.Errorf("%s: %s", e.Error, e.Message)
	}
	return out.Value, nil
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// startSafari starts safaridriver, opens a Safari automation session and
// loads url in it. Stopping ends the session, which closes its window.
func startSafari(t *testing.T, bin, url string) *running {
	t.Helper()
	port := freePort(t)
	driver := startProcess(t, bin, "-p", strconv.Itoa(port))
	base := fmt.Sprintf("http://127.0.0.1:%d", port)

	fail := func(format string, args ...any) {
		t.Helper()
		driver.stop()
		t.Fatalf(format+"\nsafaridriver output:\n%s", append(args, driver.output())...)
	}

	// Wait for the driver to answer.
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := webDriver("GET", base+"/status", nil); err == nil {
			break
		}
		select {
		case err := <-driver.exited:
			fail("safaridriver exited at startup (%v)", err)
		default:
		}
		if time.Now().After(deadline) {
			fail("safaridriver did not start on port %d", port)
		}
		time.Sleep(100 * time.Millisecond)
	}

	value, err := webDriver("POST", base+"/session", map[string]any{
		"capabilities": map[string]any{"alwaysMatch": map[string]any{"browserName": "safari"}},
	})
	if err != nil {
		fail("couldn't start a Safari session: %v\n"+
			"If remote automation is off, allow it once with: safaridriver --enable\n"+
			"(it asks for an administrator password), or in Safari turn on\n"+
			"Develop > Allow Remote Automation.", err)
	}
	var session struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(value, &session); err != nil || session.SessionID == "" {
		fail("Safari session reply has no session ID: %s", value)
	}
	sessionURL := base + "/session/" + session.SessionID

	var once sync.Once
	stop := func() {
		once.Do(func() {
			if _, err := webDriver("DELETE", sessionURL, nil); err != nil {
				t.Logf("ending the Safari session: %v", err)
			}
			driver.stop()
		})
	}
	// Navigating waits for the page to load, while the test page is already
	// reporting, so it runs in the background.
	go func() {
		if _, err := webDriver("POST", sessionURL+"/url", map[string]string{"url": url}); err != nil {
			t.Logf("Safari navigation: %v", err)
		}
	}()
	return &running{exited: driver.exited, stop: stop, output: driver.output}
}

// ---- The test ----------------------------------------------------------------

// browserFixture writes the sample tree the test page expects:
//
//	outputs/day1/fox1.png   metadata
//	outputs/day1/fox2.png   metadata, differs from fox1
//	outputs/day1/plain.png  no metadata
//	outputs/day1/.hidden.png, notes.txt  not listed
//	work/photo.jpg          a second top-level directory, with a 40x30 JPEG
func browserFixture(t *testing.T) options.Roots {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	day := filepath.Join(root, "outputs", "day1")
	writePNG(t, filepath.Join(day, "fox1.png"),
		"a photo of a red fox in the snow\nNegative prompt: blurry\n"+
			"Steps: 30, Sampler: Euler, CFG scale: 7, Seed: 1, Model: base")
	writePNG(t, filepath.Join(day, "fox2.png"),
		"a photo of a grey fox in the snow\nNegative prompt: blurry\n"+
			"Steps: 30, Sampler: Euler, CFG scale: 5, Seed: 1, Model: base")
	writePNG(t, filepath.Join(day, "plain.png"), "")
	writePNG(t, filepath.Join(day, ".hidden.png"), "")
	require.NoError(t, os.WriteFile(filepath.Join(day, "notes.txt"), []byte("x"), 0o644))
	// A JPEG, to check that Copy Image converts it to PNG.
	work := filepath.Join(root, "work")
	require.NoError(t, os.MkdirAll(work, 0o755))
	f, err := os.Create(filepath.Join(work, "photo.jpg"))
	require.NoError(t, err)
	require.NoError(t, jpeg.Encode(f, image.NewRGBA(image.Rect(0, 0, 40, 30)), nil))
	require.NoError(t, f.Close())
	return options.Roots{List: []options.Root{
		{Name: "outputs", Path: filepath.Join(root, "outputs")},
		{Name: "work", Path: filepath.Join(root, "work")},
	}}
}

func runBrowser(t *testing.T, name string) {
	b, ok := browsers[name]
	if !ok {
		t.Fatalf("unknown browser %q; use firefox, chrome or safari", name)
	}
	bin, err := b.find()
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	t.Logf("using %s", bin)

	// The app, with debug logging so the page's own errors reach the log.
	logger := slog.New(slog.NewTextHandler(t.Output(), &slog.HandlerOptions{Level: slog.LevelDebug}))
	app := Handler(Config{Roots: browserFixture(t), BackendURL: testBackendURL, Log: logger, Debug: true})

	results := make(chan stepResult, 64)
	harness, err := fs.Sub(harnessFS, "testdata/harness")
	require.NoError(t, err)
	mux := http.NewServeMux()
	mux.Handle("GET /harness/", http.StripPrefix("/harness/", http.FileServerFS(harness)))
	mux.HandleFunc("POST /harness/report", func(w http.ResponseWriter, r *http.Request) {
		var res stepResult
		if err := json.NewDecoder(r.Body).Decode(&res); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		results <- res
		w.WriteHeader(http.StatusNoContent)
	})
	mux.Handle("/", app)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), browserTimeout)
	defer cancel()
	br := b.start(t, bin, fmt.Sprintf("%s/harness/?url=%s", srv.URL, testBackendURL))
	defer br.stop()

	steps := 0
	for {
		select {
		case res := <-results:
			if res.Done {
				require.Positive(t, steps, "the test page ran no steps")
				return
			}
			steps++
			if res.OK {
				t.Logf("ok    %s", res.Step)
			} else {
				t.Errorf("FAIL  %s: %s", res.Step, res.Detail)
			}
		case err := <-br.exited:
			t.Fatalf("%s exited before the test finished (%v):\n%s", name, err, br.output())
		case <-ctx.Done():
			msg := "timed out waiting for the test page"
			if steps == 0 {
				msg += " (it never reported; did the browser load it?)"
			}
			t.Fatalf("%s: %s after %d steps\n%s", name, msg, steps, br.output())
		}
	}
}
