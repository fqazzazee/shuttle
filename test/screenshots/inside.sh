#!/usr/bin/env bash
# SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0
#
# Runs inside run.sh's private session bus: see there.
set -euo pipefail

tmp=$1 out=$2
T="tmux -L shuttle-shots"
cols=100 rows=30
mkdir -p "$HOME/.config/shuttle" "$HOME/Documents" "$HOME/Projects" "$HOME/Pictures" "$HOME/Music" \
	"$HOME/archive" "$tmp/nas/archive" "$tmp/nas/documents" "$tmp/nas/music" "$tmp/nas/photos" "$tmp/nas/projects"
for i in $(seq 40); do echo "note $i" >"$HOME/Documents/note$i.txt"; done
for i in $(seq 25); do echo "package p$i" >"$HOME/Projects/p$i.go"; done
head -c 400M /dev/urandom >"$HOME/Pictures/holiday.raw"
head -c 300M /dev/urandom >"$HOME/Music/album.flac"
cat >"$HOME/.config/shuttle/config.toml" <<TOML
[global]
bandwidth = { up = "8MB/s" }

[shares.nas]
type = "path"
path = "$tmp/nas"

[profiles.live]
mode = "sync"
triggers = ["manual"]

[profiles.backup]
mode = "push"
triggers = ["manual"]

[shuttles.archive]
profile = "live"
local = "~/archive"
remote = "nas:/archive"
enabled = false

[shuttles.documents]
profile = "live"
local = "~/Documents"
remote = "nas:/documents"

[shuttles.music]
profile = "backup"
local = "~/Music"
remote = "nas:/music"

[shuttles.photos]
profile = "backup"
local = "~/Pictures"
remote = "nas:/photos"

[shuttles.projects]
profile = "live"
local = "~/Projects"
remote = "nas:/projects"
TOML

"$tmp/shuttle" daemon 2>"$tmp/daemon.log" &
daemon=$!
trap 'kill $daemon 2>/dev/null || true' EXIT
for _ in $(seq 50); do "$tmp/shuttle" status >/dev/null 2>&1 && break; sleep 0.1; done

keys() { for k in "$@"; do $T send-keys "$k"; sleep 0.2; done; }
# shot <name> <bg> <fg>: the screen as docs/screenshots/<name>.svg.
shot() { $T capture-pane -e -N -p | "$tmp/ansisvg" -bg "$2" -fg "$3" >"$out/$1.svg"; echo "wrote $1.svg"; }
# tui <bg> <fg>: shuttle in the pane, on a terminal of that background, which
# it asks for to pick its palette. stty tab3: the renderer moves with
# spaces, not tabs, which a capture would keep.
tui() {
	$T set -g window-style "bg=$1,fg=$2"
	$T respawn-pane -k "stty tab3; TERM=xterm-256color COLORTERM=truecolor exec $tmp/shuttle"
	sleep 1.5
}

$T -f /dev/null new-session -d -x $cols -y $rows
tui '#22252d' '#c9ced8'
keys g j s; sleep 1.5      # documents: a quick sync
keys G s; sleep 1.5        # projects
keys k s; sleep 6          # photos: alone at the bandwidth cap
keys k s j; sleep 7        # music shares the cap with it; photos selected
shot dashboard-dark '#22252d' '#c9ced8'
keys '?'; sleep 0.5
shot help-dark '#22252d' '#c9ced8'
keys Escape

tui '#ffffff' '#1b1e26'
keys g j j j; sleep 8      # photos selected again, its rate drawn anew
shot dashboard-light '#ffffff' '#1b1e26'
keys '?'; sleep 0.5
shot help-light '#ffffff' '#1b1e26'
