#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-2.0-or-later
#
# The GNOME extension in a headless GNOME Shell 50, against a running daemon.
# Everything runs on a private session bus, with HOME and every XDG folder in
# a temp folder and GSettings in a key file there, and without the login
# session's variables: the user's own shell, extensions, settings and keyring
# are never touched, and a broken extension costs nothing. --unsafe-mode lets
# the test read the shell's state through org.gnome.Shell.Eval.
set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
tmp=$(mktemp -d)
# The session's document portal mounts $XDG_RUNTIME_DIR/doc, and may still be
# letting go of it when the session ends.
trap 'fusermount3 -uz "$tmp/run/doc" 2>/dev/null || true; rm -rf "$tmp"' EXIT
(cd "$root" && CGO_ENABLED=0 go build -o "$tmp/shuttle" ./cmd/shuttle)
mkdir -m 700 "$tmp/run"
env -u DISPLAY -u WAYLAND_DISPLAY -u DBUS_SESSION_BUS_ADDRESS \
	-u XDG_SESSION_ID -u XDG_SESSION_TYPE -u XDG_SEAT -u XDG_VTNR \
	HOME="$tmp/home" XDG_CONFIG_HOME="$tmp/home/.config" XDG_DATA_HOME="$tmp/home/.local/share" \
	XDG_STATE_HOME="$tmp/home/.local/state" XDG_CACHE_HOME="$tmp/home/.cache" XDG_RUNTIME_DIR="$tmp/run" \
	GSETTINGS_BACKEND=keyfile \
	dbus-run-session -- "$root/test/extension/inside.sh" "$root" "$tmp" 2>"$tmp/session.log" || {
	# The session's services write to its stderr at length: shown only when
	# something went wrong.
	echo "--- session log"; tail -40 "$tmp/session.log"
	exit 1
}
