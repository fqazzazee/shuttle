#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-2.0-or-later
#
# Runs inside run.sh's private session bus: see there.
set -euo pipefail

tmp=$1
shuttle=$tmp/shuttle
share=$tmp/share
T="tmux -L shuttle-exit"
mkdir -p "$HOME/Documents" "$HOME/.config/shuttle" "$share/docs"
: >"$HOME/.config/shuttle/config.toml" # empty: the TUI writes the rest
echo "first" >"$HOME/Documents/notes.txt"

step() { printf '\n== %s\n' "$*"; }
screen() { $T capture-pane -p; }
fail() {
	echo "FAIL: $*"
	echo "--- screen"; screen || true
	echo "--- config"; cat "$HOME/.config/shuttle/config.toml" || true
	echo "--- daemon log"; tail -20 "$tmp/daemon.log" || true
	exit 1
}
# shows <text>: wait until the screen shows text.
shows() {
	for _ in $(seq 50); do screen | grep -qF -- "$1" && return 0; sleep 0.2; done
	fail "the screen never showed: $1"
}
keys() { for k in "$@"; do $T send-keys "$k"; sleep 0.1; done; }
typed() { $T send-keys -l "$1"; sleep 0.2; }

"$shuttle" daemon 2>"$tmp/daemon.log" &
daemon=$!
trap 'kill $daemon 2>/dev/null || true' EXIT
for _ in $(seq 50); do "$shuttle" status >/dev/null 2>&1 && break; sleep 0.1; done
$T new-session -d -x 150 -y 32 "TERM=xterm-256color $shuttle"
shows "1 Shuttles"

step "shares: add one, test it, save it"
keys 5
shows "No shares yet"
keys n; typed disk; keys Tab Left Tab  # type: path
typed "$share"
keys C-t
shows "it works"
keys C-s
shows "saved the share disk"
grep -q "^path = \"$share\"$" "$HOME/.config/shuttle/config.toml" || fail "the share is not in the config"
shows "This computer: "  # how much room is left here, under the shares
screen | grep -qE "^disk +path .+ [0-9.]+ [kMGT]?B of [0-9.]+ [kMGT]?B" || fail "the mounted share's free space is not shown"

step "profiles: add one"
keys 4 n; typed live; keys Tab Right Right Right  # mode: sync
keys Tab Tab; keys Left # triggers, conflicts (every, at, poll need triggers): ask
keys C-s
shows "saved the profile live"
shows "live  sync"
grep -q '^conflicts = "ask"$' "$HOME/.config/shuttle/config.toml" || fail "the profile's conflicts setting is not in the config"

step "shuttles: add one, with a setting of its own"
keys 1 n; typed docs; keys Enter
shows "Folder on disk: /"
keys o; shows "Folder on disk: /docs"; keys Enter
shows "Folder here: $HOME"
keys Right; shows "Folder here: $HOME/Documents"; keys Enter
shows "▸ none"
keys Down Enter
shows "from live: sync"
# Down to notify.completed, past the rows that do not apply to a sync shuttle
# with no triggers and trash versions: every, at, poll, deletions, keep last.
for _ in $(seq 13); do keys Down; done; keys Left Down  # notify.completed: off
shows " ●Tell done      off"
shows "from live: ask"
keys C-s
shows "saved the shuttle docs"
grep -q '^notify = { completed = false }$' "$HOME/.config/shuttle/config.toml" || fail "the override is not in the config"

step "the dashboard: the daemon follows the new config; sync from the TUI"
shows "docs  ↔ sync"
keys s
shows "syncing docs"
for _ in $(seq 50); do [ -f "$share/docs/notes.txt" ] && break; sleep 0.2; done
[ "$(cat "$share/docs/notes.txt" 2>/dev/null)" = first ] || fail "notes.txt was not synced"
shows "just now"
shows "6 B       1         just now"  # what docs syncs: notes.txt, "first\n"

step "conflicts: one waits for the user, who keeps this computer's copy"
echo "mine" >"$HOME/Documents/notes.txt"
echo "theirs, edited on the share" >"$share/docs/notes.txt"
keys s
shows "2 Conflicts (1)"
keys 2
shows "notes.txt"
keys l
shows "Keep this computer's copy of notes.txt and replace the share's? (y/n)"
keys y
shows "kept this computer's copy of notes.txt"
for _ in $(seq 50); do [ "$(cat "$share/docs/notes.txt")" = mine ] && break; sleep 0.2; done
[ "$(cat "$share/docs/notes.txt")" = mine ] || fail "the share's copy was not replaced"
shows "Nothing waiting for you"

step "activity: what the shuttle did"
keys 3
shows "notes.txt: resolved: kept this computer's copy"
shows "notes.txt: copied to the share"

step "what to sync: leave a folder out by marking it on the share"
mkdir -p "$share/docs/raw"
echo "left out" >"$share/docs/raw/a.cr2"
echo "kept" >"$share/docs/kept.txt"
reloads=$(grep -c "config reloaded" "$tmp/daemon.log" || true)
keys 1 e Enter
shows "Folder on disk: /docs"
shows "raw/"
keys m m  # all but what is marked
shows "Nothing marked yet"
keys Space  # raw, the first: folders come first
shows "Left out: raw/"
shows "[ ] raw/"
keys Enter Enter Enter C-s
shows "saved the shuttle docs"
grep -q '^exclude = \["raw"\]$' "$HOME/.config/shuttle/config.toml" || fail "the mark is not in the config"
for _ in $(seq 50); do [ "$(grep -c "config reloaded" "$tmp/daemon.log")" -gt "$reloads" ] && break; sleep 0.2; done
keys s
for _ in $(seq 50); do [ -f "$HOME/Documents/kept.txt" ] && break; sleep 0.2; done
[ -f "$HOME/Documents/kept.txt" ] || fail "kept.txt was not synced"
[ -e "$HOME/Documents/raw" ] && fail "the folder left out was synced"
[ -f "$share/docs/raw/a.cr2" ] || fail "the folder left out was touched on the share"

step "quit: the daemon carries on"
keys q
sleep 0.5
$T has-session 2>/dev/null && fail "the TUI did not quit"
"$shuttle" status | grep -q '^docs ' || fail "the daemon stopped with the TUI"

printf '\nPASS: Phase 4 exit condition: the TUI against a running daemon\n'
