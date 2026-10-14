#!/usr/bin/env bash
# SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0
#
# The README's screenshots: the real binary in tmux against a running
# daemon, on a private session bus, with HOME and every XDG folder in a
# temp folder, as test/tui does; its screens captured and drawn as SVG by
# ansisvg into docs/screenshots. The data is made up, in the temp folder:
# no real share, folder or name shows.
set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
tmp=$(mktemp -d)
run=$(mktemp -d) # short: tmux's socket path has a length limit
trap 'tmux -L shuttle-shots kill-server 2>/dev/null || true; rm -rf "$tmp" "$run"' EXIT
(cd "$root" && CGO_ENABLED=0 go build -o "$tmp/shuttle" ./cmd/shuttle && go build -o "$tmp/ansisvg" ./test/screenshots/ansisvg)
chmod 700 "$run"
env -u DISPLAY -u WAYLAND_DISPLAY -u DBUS_SESSION_BUS_ADDRESS -u TMUX \
	HOME="$tmp/home" XDG_CONFIG_HOME="$tmp/home/.config" XDG_STATE_HOME="$tmp/home/.local/state" \
	XDG_RUNTIME_DIR="$run" TMUX_TMPDIR="$run" \
	dbus-run-session -- "$root/test/screenshots/inside.sh" "$tmp" "$root/docs/screenshots"
