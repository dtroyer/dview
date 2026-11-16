BINARY := dview

# Browsers for test-browser: firefox, chrome, safari, or a comma-separated
# list. Safari needs remote automation allowed once: safaridriver --enable
BROWSER ?= firefox

.PHONY: all build test test-browser clean

all: build

build:
	go build -o bin/$(BINARY) .

test:
	go test ./...

# Runs the web UI in a real browser. The browser must be installed; set
# DVIEW_FIREFOX_PATH, DVIEW_CHROME_PATH or DVIEW_SAFARIDRIVER_PATH if it
# isn't found.
test-browser:
	DVIEW_TEST_BROWSER=$(BROWSER) go test -tags browser -count=1 -run '^TestBrowser$$' -v ./internal/server/

clean:
	rm -rf bin
