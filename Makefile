BINARY  := shuttle
PREFIX  ?= $(HOME)/.local
BINDIR  ?= $(PREFIX)/bin
# Found by systemd from ~/.local/share/systemd/user for a user install;
# packages set /usr/lib/systemd/user.
USERUNITDIR ?= $(PREFIX)/share/systemd/user
APPDIR      ?= $(PREFIX)/share/applications
ICONDIR     ?= $(PREFIX)/share/icons/hicolor
DESKTOP     := io.github.fqazzazee.Shuttle.desktop
# The app icon, and its symbolic form: the extension's own, one file.
APPICON     := packaging/icons/io.github.fqazzazee.Shuttle.svg
SYMICON     := extension/icons/shuttle-symbolic.svg
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

# Pinned to match CI. `go run` builds it on first use, so no separate install.
GOLANGCI_LINT ?= go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
# Keep in step with the release workflow. Not newer: from v2.18 it needs Go 1.27.
GORELEASER    ?= go run github.com/goreleaser/goreleaser/v2@v2.17.1

.PHONY: build test lint fmt-check install uninstall smoke unit-check clean samba-up samba-down test-integration test-nfs test-keyring test-systemd test-tui screenshots test-extension lint-extension install-extension uninstall-extension package-files packages test-packages

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

# Installs; enabling and starting are left to the user (the message says how).
install: build
	install -Dm755 bin/$(BINARY) $(DESTDIR)$(BINDIR)/$(BINARY)
	install -d $(DESTDIR)$(USERUNITDIR)
	sed 's|@BINDIR@|$(BINDIR)|g' packaging/systemd/shuttle.service.in > $(DESTDIR)$(USERUNITDIR)/shuttle.service
	install -d $(DESTDIR)$(APPDIR)
	sed 's|@BINDIR@|$(BINDIR)|g' packaging/desktop/$(DESKTOP).in > $(DESTDIR)$(APPDIR)/$(DESKTOP)
	install -Dm644 $(APPICON) $(DESTDIR)$(ICONDIR)/scalable/apps/io.github.fqazzazee.Shuttle.svg
	install -Dm644 $(SYMICON) $(DESTDIR)$(ICONDIR)/symbolic/apps/io.github.fqazzazee.Shuttle-symbolic.svg
	@echo "Installed. To run it with your desktop session:"
	@echo "  systemctl --user daemon-reload && systemctl --user enable --now shuttle"
	@echo "Then run shuttle to add a share and a shuttle; the service picks them up."

uninstall:
	rm -f $(DESTDIR)$(BINDIR)/$(BINARY) $(DESTDIR)$(USERUNITDIR)/shuttle.service $(DESTDIR)$(APPDIR)/$(DESKTOP) \
		$(DESTDIR)$(ICONDIR)/scalable/apps/io.github.fqazzazee.Shuttle.svg \
		$(DESTDIR)$(ICONDIR)/symbolic/apps/io.github.fqazzazee.Shuttle-symbolic.svg

# Checks the systemd unit and the desktop file as installed. systemd-analyze
# and desktop-file-validate only warn about a bad setting, so any output at
# all fails the check.
unit-check:
	@tmp=$$(mktemp -d) && trap 'rm -rf "$$tmp"' EXIT && \
	$(MAKE) -s install PREFIX="$$tmp" >/dev/null && \
	out=$$(systemd-analyze --user verify "$$tmp/share/systemd/user/shuttle.service" 2>&1) ; \
	if [ -n "$$out" ]; then echo "$$out"; exit 1; fi; echo "shuttle.service: ok" && \
	out=$$(desktop-file-validate "$$tmp/share/applications/$(DESKTOP)" 2>&1) ; \
	if [ -n "$$out" ]; then echo "$$out"; exit 1; fi; echo "$(DESKTOP): ok"

# Runs the installed copy from outside the source tree, so a binary that only
# works from a checkout fails here.
smoke:
	cd / && "$(DESTDIR)$(BINDIR)/$(BINARY)" version
	cd / && "$(DESTDIR)$(BINDIR)/$(BINARY)" help >/dev/null
	grep -qx 'ExecStart=$(BINDIR)/$(BINARY) daemon' "$(DESTDIR)$(USERUNITDIR)/shuttle.service"

# The packages: RPM, DEB and tarballs for amd64 and arm64 in dist/, as a tagged
# release would make them, but with a snapshot version and nothing published.
packages:
	$(GORELEASER) release --snapshot --clean

# Installs the amd64 RPM and DEB from `make packages` in throwaway Fedora 44 and
# Debian containers, checks what they put where, and removes them again.
test-packages:
	PODMAN="$(PODMAN)" test/packages/run.sh

# The files the packages carry besides the binary, filled in for /usr, in
# build/package; goreleaser runs this first.
PKGDIR := build/package
package-files:
	rm -rf $(PKGDIR)
	mkdir -p $(PKGDIR)
	sed 's|@BINDIR@|/usr/bin|g' packaging/systemd/shuttle.service.in > $(PKGDIR)/shuttle.service
	sed 's|@BINDIR@|/usr/bin|g' packaging/desktop/$(DESKTOP).in > $(PKGDIR)/$(DESKTOP)
	cp -r extension $(PKGDIR)/extension
	rm -rf $(PKGDIR)/extension/test
	packaging/notices.sh > $(PKGDIR)/THIRD_PARTY_NOTICES
	# Debian keeps every license in one copyright file, the one doc file
	# that minimal images do not leave out.
	{ cat LICENSE; printf '\n%s\n== The GNOME Shell extension: GPL-2.0-or-later\n\n' "$$(printf '=%.0s' $$(seq 78))"; \
	  cat extension/LICENSE; cat $(PKGDIR)/THIRD_PARTY_NOTICES; } > $(PKGDIR)/copyright

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

# The engine suite with its remote side on an in-process NFSv3 server
# (willscott/go-nfs) instead of a local folder. The fsys contract over NFS
# already runs in `make test`.
test-nfs:
	SHUTTLE_TEST_NFS=inprocess go test -race -count=1 ./internal/engine/

# Keyring tests run in a private D-Bus session with a throwaway GNOME Keyring;
# the user's own keyring is never touched. Needs dbus-run-session and
# gnome-keyring-daemon.
test-keyring:
	internal/secrets/testdata/with-test-keyring.sh go test -race -count=1 -v ./internal/secrets/

# Phase 3's exit condition under a real systemd: a throwaway container with
# systemd as PID 1 runs the unit, every trigger, kill -9 mid-run and a broken
# config. About three minutes. PODMAN="sudo podman" where rootless fails.
test-systemd:
	PODMAN="$(PODMAN)" test/systemd/run.sh

# Phase 4's exit test: the TUI, in tmux, against a running daemon on a
# private session bus. Needs tmux and dbus-run-session.
test-tui:
	test/tui/run.sh

# The README's screenshots in docs/screenshots, the same way as test-tui:
# a scratch daemon, made-up data, the TUI in tmux; drawn as SVG.
screenshots:
	test/screenshots/run.sh

# The GNOME extension, for the user running make: GNOME Shell finds it in
# ~/.local/share/gnome-shell/extensions. On Wayland the shell picks up a newly
# installed extension at the next login; then `gnome-extensions enable`.
EXT_UUID := shuttle@fqazzazee.github.io
EXTDIR   ?= $(HOME)/.local/share/gnome-shell/extensions

install-extension:
	@test -n "$(EXTDIR)" || { echo "EXTDIR is empty"; exit 1; }
	rm -rf "$(DESTDIR)$(EXTDIR)/$(EXT_UUID)"
	install -d "$(DESTDIR)$(EXTDIR)/$(EXT_UUID)"
	cp -r extension/. "$(DESTDIR)$(EXTDIR)/$(EXT_UUID)/"
	rm -rf "$(DESTDIR)$(EXTDIR)/$(EXT_UUID)/test"
	@echo "Installed. Log out and back in (Wayland), then:"
	@echo "  gnome-extensions enable $(EXT_UUID)"

uninstall-extension:
	@test -n "$(EXTDIR)" || { echo "EXTDIR is empty"; exit 1; }
	rm -rf "$(DESTDIR)$(EXTDIR)/$(EXT_UUID)"

# The GNOME extension's lint: eslint from package-lock.json, installed with
# no package's install scripts run. Needs node 20.19+ and npm.
lint-extension: node_modules
	npx eslint extension/

node_modules: package.json package-lock.json
	npm ci --ignore-scripts --no-audit --no-fund
	@touch node_modules

# The GNOME extension: its unit tests in plain gjs, then the extension in a
# headless GNOME Shell 50 against a running daemon, on a private session bus
# with every XDG folder in a temp folder. Needs gnome-shell, gjs and
# dbus-run-session.
test-extension:
	gjs -m extension/test/run.js
	test/extension/run.sh

PODMAN ?= podman

clean:
	rm -rf bin/ build/ dist/ coverage.out
