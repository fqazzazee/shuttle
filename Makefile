BINARY  := shuttle
PREFIX  ?= $(HOME)/.local
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

# Pinned to match CI. `go run` builds it on first use, so no separate install.
GOLANGCI_LINT ?= go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0

.PHONY: build test lint fmt-check install uninstall smoke clean samba-up samba-down test-integration test-keyring

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/$(BINARY) ./cmd/shuttle

test:
	go test -race ./...

lint: fmt-check
	go vet ./...
	$(GOLANGCI_LINT) run

fmt-check:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then echo "not gofmt'd:"; echo "$$unformatted"; exit 1; fi

install: build
	install -Dm755 bin/$(BINARY) $(DESTDIR)$(PREFIX)/bin/$(BINARY)

uninstall:
	rm -f $(DESTDIR)$(PREFIX)/bin/$(BINARY)

# Runs the installed copy from outside the source tree, so a binary that only
# works from a checkout fails here.
smoke:
	cd / && "$(DESTDIR)$(PREFIX)/bin/$(BINARY)" version
	cd / && "$(DESTDIR)$(PREFIX)/bin/$(BINARY)" help >/dev/null

# SMB integration tests run against a throwaway Samba container.
CONTAINER    ?= podman
SAMBA_IMAGE  := shuttle-test-samba
SAMBA_PORT   ?= 1445
# The share's files, on the host: tests set up and inspect them directly.
SAMBA_DIR    ?= $(CURDIR)/.cache/samba-share
SHELL        := /bin/bash # samba-up waits on /dev/tcp
SMB_TEST_ENV := SHUTTLE_TEST_SMB_ADDR=127.0.0.1:$(SAMBA_PORT) SHUTTLE_TEST_SMB_DIR=$(SAMBA_DIR)
# Rootless podman: map the host user to the same UID inside, so files Samba
# writes belong to whoever runs the tests. Rootful docker needs nothing.
USERNS       := $(if $(filter podman,$(CONTAINER)),--userns=keep-id --user root)

samba-up:
	mkdir -p $(SAMBA_DIR)
	$(CONTAINER) build -q -t $(SAMBA_IMAGE) --build-arg UID=$$(id -u) \
		-f internal/fsys/testdata/samba/Containerfile internal/fsys/testdata/samba
	$(CONTAINER) run -d --rm --name shuttle-samba $(USERNS) -p 127.0.0.1:$(SAMBA_PORT):445 \
		-v $(SAMBA_DIR):/share:z $(SAMBA_IMAGE)
	@for i in $$(seq 30); do \
		(exec 3<>/dev/tcp/127.0.0.1/$(SAMBA_PORT)) 2>/dev/null && exit 0; sleep 1; \
	done; $(CONTAINER) logs shuttle-samba; echo "samba did not come up"; exit 1

samba-down:
	-$(CONTAINER) stop shuttle-samba

# Runs every test, including those that need the Samba server and a Secret
# Service (a throwaway one; the user's own keyring is never touched). Needs
# `make samba-up` first, plus dbus-run-session and gnome-keyring-daemon.
test-integration:
	$(SMB_TEST_ENV) internal/secrets/testdata/with-test-keyring.sh go test -race -count=1 ./...

# Keyring tests run in a private D-Bus session with a throwaway GNOME Keyring;
# the user's own keyring is never touched. Needs dbus-run-session and
# gnome-keyring-daemon.
test-keyring:
	internal/secrets/testdata/with-test-keyring.sh go test -race -count=1 -v ./internal/secrets/

clean:
	rm -rf bin/ dist/ coverage.out
