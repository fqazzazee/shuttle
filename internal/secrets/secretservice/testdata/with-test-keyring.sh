#!/usr/bin/env bash
# Runs a command inside a private D-Bus session with a throwaway, unlocked
# GNOME Keyring whose files live in a temp folder. The user's own session
# bus and keyring are never touched.
set -euo pipefail

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
mkdir -m 700 "$tmp/run"

# Keep Go's caches: HOME is about to point at the temp folder.
export GOCACHE="$(go env GOCACHE)" GOMODCACHE="$(go env GOMODCACHE)" GOPATH="$(go env GOPATH)"
export HOME="$tmp" XDG_DATA_HOME="$tmp/data" XDG_CONFIG_HOME="$tmp/config" XDG_RUNTIME_DIR="$tmp/run"
# No display either: an unlock prompt from the throwaway keyring must fail at
# once, never appear on the user's screen.
unset DBUS_SESSION_BUS_ADDRESS DISPLAY WAYLAND_DISPLAY XAUTHORITY

# SHUTTLE_TEST_KEYRING=isolated is what lets the keyring tests run at all.
exec dbus-run-session -- bash -c '
	printf test | gnome-keyring-daemon --unlock --components=secrets >/dev/null
	export SHUTTLE_TEST_KEYRING=isolated
	exec "$@"
' _ "$@"
