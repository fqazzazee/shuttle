#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-2.0-or-later
#
# Phase 4's exit condition: everything in SPEC.md's TUI section works
# against a running daemon. The real binary runs in tmux, on a private
# session bus, with HOME and every XDG folder in a temp folder: the user's
# own session, files and keyring are never touched. It starts from an empty
# config and does what a user would: adds a share (and tests it), a profile,
# and a shuttle with a setting of its own; syncs; resolves a conflict; reads
# the activity log.
set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
tmp=$(mktemp -d)
trap 'tmux -L shuttle-exit kill-server 2>/dev/null || true; rm -rf "$tmp"' EXIT
(cd "$root" && CGO_ENABLED=0 go build -o "$tmp/shuttle" ./cmd/shuttle)
mkdir -m 700 "$tmp/run"
exec env -u DISPLAY -u WAYLAND_DISPLAY -u DBUS_SESSION_BUS_ADDRESS \
	HOME="$tmp/home" XDG_CONFIG_HOME="$tmp/home/.config" XDG_STATE_HOME="$tmp/home/.local/state" \
	XDG_RUNTIME_DIR="$tmp/run" TMUX_TMPDIR="$tmp/run" \
	dbus-run-session -- "$root/test/tui/inside.sh" "$tmp"
