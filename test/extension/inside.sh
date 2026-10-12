#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-2.0-or-later
#
# Runs inside run.sh's private session bus: see there.
set -euo pipefail

root=$1
tmp=$2
shuttle=$tmp/shuttle
uuid=shuttle@fqazzazee.github.io
log=$tmp/shell.log
indicator="Main.panel.statusArea['$uuid']"

step() { printf '\n== %s\n' "$*"; }
fail() {
	echo "FAIL: $*"
	echo "--- shell log"; grep -iE "js (error|warning)|$uuid|shuttle" "$log" | tail -30 || true
	echo "--- daemon log"; tail -20 "$tmp/daemon.log" 2>/dev/null || true
	exit 1
}
# ev <js>: the value of a JavaScript expression in the shell, as JSON.
ev() {
	local out
	out=$(gdbus call --session --dest org.gnome.Shell --object-path /org/gnome/Shell \
		--method org.gnome.Shell.Eval "$1" 2>/dev/null) || return 1
	[[ $out == "(true, '"* ]] || return 1
	out=${out#"(true, '"}
	echo "${out%"')"}"
}
# is <js> <json>: wait until the expression has that value.
is() {
	local got=
	for _ in $(seq 50); do
		got=$(ev "$1") || true
		[ "$got" = "$2" ] && return 0
		sleep 0.2
	done
	fail "$1 never became $2 (last: ${got:-no answer})"
}
shows() { is "$indicator?.shownState ?? null" "\"$1\""; }
# row <name> <js on r>: wait until the dropdown's row for name satisfies it.
row() { is "(r => !!r && ($2))($indicator.shownRows.find(r => r.name === '$1'))" true; }
rows() { is "$indicator.shownRows.map(r => r.name).join(',')" "\"$1\""; }
# snap <name>: with SHUTTLE_EXT_SCREENSHOTS set to a folder, a picture of
# the open dropdown there, to look at.
snap() {
	[ -n "${SHUTTLE_EXT_SCREENSHOTS:-}" ] || return 0
	ev "$indicator.menu.open(false)" >/dev/null
	sleep 0.5
	gdbus call --session --dest org.gnome.Shell --object-path /org/gnome/Shell/Screenshot \
		--method org.gnome.Shell.Screenshot.Screenshot false false "$SHUTTLE_EXT_SCREENSHOTS/$1.png" >/dev/null ||
		fail "no screenshot"
	ev "$indicator.menu.close(false)" >/dev/null
}

step "the extension, installed in this session's own data folder"
dest=$XDG_DATA_HOME/gnome-shell/extensions/$uuid
mkdir -p "$dest"
cp -r "$root/extension/metadata.json" "$root/extension/extension.js" "$root/extension/stylesheet.css" \
	"$root/extension/lib" "$root/extension/icons" "$dest/"
gsettings set org.gnome.shell enabled-extensions "['$uuid']"

mkdir -p "$HOME/Documents" "$HOME/Pictures" "$tmp/share/docs" "$tmp/share/pics" "$XDG_CONFIG_HOME/shuttle"
echo "a" >"$HOME/Documents/a.txt"
# config [pics]: two shuttles, or only docs.
config() {
	cat >"$XDG_CONFIG_HOME/shuttle/config.toml" <<-EOF
		[shares.disk]
		type = "path"
		path = "$tmp/share"

		[shuttles.docs]
		mode = "push"
		local = "$HOME/Documents"
		remote = "disk:docs"
		bandwidth = { up = "64KiB/s" } # slow enough to watch a run
	EOF
	if [ "${1:-}" = pics ]; then
		cat >>"$XDG_CONFIG_HOME/shuttle/config.toml" <<-EOF

			[shuttles.pics]
			mode = "sync"
			local = "$HOME/Pictures"
			remote = "disk:pics"
		EOF
	fi
}
config pics

gnome-shell --headless --wayland --no-x11 --virtual-monitor 1280x800 --unsafe-mode >"$log" 2>&1 &
shell=$!
daemon=
trap 'kill $daemon $shell 2>/dev/null || true; wait 2>/dev/null || true' EXIT
for _ in $(seq 100); do ev true >/dev/null && break; sleep 0.2; done
ev true >/dev/null || fail "the shell did not start"
echo "GNOME Shell $(ev Config.PACKAGE_VERSION)"

step "enabled, with no daemon: not running"
gnome-extensions info "$uuid" | grep -q "State: ACTIVE" || fail "the extension is not active: $(gnome-extensions info "$uuid")"
shows not-running
is "$indicator.shownMessage" '"The shuttle service is not running"'
rows ""

step "the daemon starts: up to date, with the extension's own icon"
"$shuttle" daemon 2>"$tmp/daemon.log" &
daemon=$!
shows idle
is "$indicator._icon.gicon.get_file?.().query_exists(null) ?? false" true

step "the dropdown: every shuttle by name, with its mode and when it last ran"
rows "docs,pics"
is "$indicator.shownMessage" '""'
row docs "r.title === 'docs  →' && r.when === 'never' && r.detail === ''"
row pics "r.title === 'pics  ↔'"
"$shuttle" sync docs >/dev/null
for _ in $(seq 50); do [ -f "$tmp/share/docs/a.txt" ] && break; sleep 0.1; done
[ -f "$tmp/share/docs/a.txt" ] || fail "the sync did not push"
shows idle
row docs "r.when === 'just now'"
is "(m => { m.open(false); m.close(false); return true; })($indicator.menu)" true

step "paused, and resumed"
"$shuttle" pause >/dev/null
shows paused
"$shuttle" resume >/dev/null
shows idle

step "syncing: the icon pulses, and the upload rate shows beside it"
head -c 200000 /dev/urandom >"$HOME/Documents/big.bin"
"$shuttle" sync docs >/dev/null
shows running
is "$indicator.pulsing" true
is "$indicator._icon.get_transition('opacity') !== null" true
is "$indicator.shownRates.startsWith('↑ ') && $indicator.shownRates.endsWith('/s')" true
row docs "r.state === 'running' && r.when === 'now' && r.detail.startsWith('syncing ')"
snap syncing
shows idle
is "$indicator.pulsing" false
is "$indicator.shownRates" '""'
is "$indicator._icon.opacity" 255
is "$indicator._icon.get_transition('opacity') === null" true
[ -f "$tmp/share/docs/big.bin" ] || fail "the sync did not push big.bin"
row docs "r.state === 'idle' && r.detail === ''"

step "a shuttle taken out of the config leaves the dropdown, and comes back"
config
rows docs
config pics
rows "docs,pics"

step "a run that fails"
rm -rf "$tmp/share"
"$shuttle" sync docs >/dev/null || true
shows error
row docs "r.state === 'error' && r.detail.includes('is the share mounted?')"
snap error

step "the daemon stops: not running"
kill "$daemon"
wait "$daemon" 2>/dev/null || true
daemon=
shows not-running
rows ""
is "$indicator.shownMessage" '"The shuttle service is not running"'

step "disabled leaves nothing behind; enabled again, it is back"
gnome-extensions disable "$uuid"
is "$indicator === undefined" true
gnome-extensions enable "$uuid"
shows not-running

step "no errors from the extension in the shell's log"
if grep -iE "js (error|warning)" "$log" | grep -q "$uuid"; then
	fail "the shell logged errors from the extension"
fi

printf '\nPASS: the extension in a headless GNOME Shell, against the daemon\n'
