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
toggle="Main.extensionManager.lookup('$uuid').stateObj._quickSettings.toggle"

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
# shows <state>: wait until the indicator shows that state: the bus, always,
# and beside it the state's icon (none while syncing: the bus pulses).
shows() {
	is "$indicator?.shownState ?? null" "\"$1\""
	local badge
	case $1 in
	idle) badge=object-select-symbolic ;;
	running) badge= ;;
	paused) badge=media-playback-pause-symbolic ;;
	held) badge=dialog-warning-symbolic ;;
	error) badge=dialog-error-symbolic ;;
	offline) badge=network-offline-symbolic ;;
	not-running) badge=action-unavailable-symbolic ;;
	esac
	is "$indicator._icon.gicon.get_file?.()?.get_basename() ?? ''" '"shuttle-symbolic.svg"'
	is "$indicator.shownBadge" "\"$badge\""
	is "$indicator._badge.visible" "$([ -n "$badge" ] && echo true || echo false)"
	snap "bar-$1" none
}
# row <name> <js on r>: wait until the dropdown's row for name satisfies it.
row() { is "(r => !!r && ($2))($indicator.shownRows.find(r => r.name === '$1'))" true; }
rows() { is "$indicator.shownRows.map(r => r.name).join(',')" "\"$1\""; }
# item <name> <js on i>: the same for the Quick Settings toggle's menu.
item() { is "(i => !!i && ($2))($toggle.shownItems.find(i => i.name === '$1'))" true; }
# open_from <menu>: click Open shuttle in that menu, and wait for the app.
open_from() {
	rm -f "$tmp/opened"
	is "$1.menu._getMenuItems().some(i => i.label?.text === 'Open shuttle')" true
	ev "$1.menu._getMenuItems().find(i => i.label?.text === 'Open shuttle').activate(null)" >/dev/null
	for _ in $(seq 50); do [ -f "$tmp/opened" ] && break; sleep 0.1; done
	[ -f "$tmp/opened" ] || fail "Open shuttle did not open the app"
}
# status <name> <state>: wait until `shuttle status` gives that shuttle that state.
status() {
	for _ in $(seq 50); do "$shuttle" status | grep -qE "^$1 +$2 " && return 0; sleep 0.2; done
	fail "shuttle status never said $1 is $2: $("$shuttle" status)"
}
# snap <name> [<menus>]: with SHUTTLE_EXT_SCREENSHOTS set to a folder, a
# picture there of the menus open (the dropdown unless named), to look at.
snap() {
	[ -n "${SHUTTLE_EXT_SCREENSHOTS:-}" ] || return 0
	local menus=${2:-$indicator.menu} m
	[ "$menus" = none ] && menus=
	[ -e "$SHUTTLE_EXT_SCREENSHOTS/$1.png" ] && return 0 # the first time is enough
	for m in $menus; do ev "$m.open(false)" >/dev/null; done
	sleep 0.5
	gdbus call --session --dest org.gnome.Shell --object-path /org/gnome/Shell/Screenshot \
		--method org.gnome.Shell.Screenshot.Screenshot false false "$SHUTTLE_EXT_SCREENSHOTS/$1.png" >/dev/null ||
		fail "no screenshot"
	for m in $menus; do ev "$m.close(false)" >/dev/null; done
}

step "the extension, installed in this session's own data folder"
dest=$XDG_DATA_HOME/gnome-shell/extensions/$uuid
mkdir -p "$dest"
# Everything the extension is made of, without its unit tests.
cp -r "$root/extension/." "$dest/"
rm -rf "${dest:?}/test"
gsettings set org.gnome.shell enabled-extensions "['$uuid']"
# "Open shuttle" opens this app: here, a stand-in that leaves a mark.
mkdir -p "$XDG_DATA_HOME/applications"
cat >"$XDG_DATA_HOME/applications/io.github.fqazzazee.Shuttle.desktop" <<EOF
[Desktop Entry]
Type=Application
Name=Shuttle
Exec=touch $tmp/opened
Terminal=false
EOF

mkdir -p "$HOME/Documents" "$HOME/Pictures" "$HOME/Music" "$tmp/share/docs" "$tmp/share/pics" "$XDG_CONFIG_HOME/shuttle"
echo "a" >"$HOME/Documents/a.txt"
# config [pics] [far]: docs, and the shuttles named: pics, which syncs and
# holds conflicts for the user, and far, on an NFS server that is not there.
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
	if [[ " $* " == *" pics "* ]]; then
		cat >>"$XDG_CONFIG_HOME/shuttle/config.toml" <<-EOF

			[shuttles.pics]
			mode = "sync"
			local = "$HOME/Pictures"
			remote = "disk:pics"
			conflicts = "ask"
		EOF
	fi
	if [[ " $* " == *" far "* ]]; then
		# Port 1: nothing listens, so the connection is refused.
		cat >>"$XDG_CONFIG_HOME/shuttle/config.toml" <<-EOF

			[shares.gone]
			type = "nfs3"
			host = "127.0.0.1"
			port = 1
			mount_port = 1
			export = "/export"

			[shuttles.far]
			mode = "push"
			local = "$HOME/Music"
			remote = "gone:"
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
# The shell opens on the overview, which hides the top bar's icons: once it
# has finished starting up, leave it for the desktop, as a user would.
is "Main.layoutManager._startingUp" false
ev "Main.overview.hide()" >/dev/null
is "Main.overview.visible || Main.overview.animationInProgress" false
echo "GNOME Shell $(ev Config.PACKAGE_VERSION)"

step "enabled, with no daemon: not running"
gnome-extensions info "$uuid" | grep -q "State: ACTIVE" || fail "the extension is not active: $(gnome-extensions info "$uuid")"
shows not-running
is "$indicator.shownMessage" '"The shuttle service is not running"'
rows ""
is "$toggle.checked" false
is "$toggle.subtitle" '"The shuttle service is not running"'

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

step "Quick Settings: on, a switch per shuttle, and Sync now syncs"
is "$toggle.checked" true
is "$toggle.subtitle" '"Up to date"'
is "$toggle.shownItems.map(i => i.name + ':' + i.on).join(',')" '"docs:true,pics:true"'
item docs "i.label === 'docs  →'"
ev "$toggle._items.get('docs')._button.emit('clicked', 1)" >/dev/null
for _ in $(seq 50); do [ -f "$tmp/share/docs/a.txt" ] && break; sleep 0.1; done
[ -f "$tmp/share/docs/a.txt" ] || fail "the sync did not push"
shows idle
row docs "r.when === 'just now'"
is "(m => { m.open(false); m.close(false); return true; })($indicator.menu)" true

step "Quick Settings: the toggle pauses every shuttle, and resumes them"
ev "$toggle.emit('clicked', 1)" >/dev/null
shows paused
is "$toggle.checked" false
is "$toggle.subtitle" '"Paused"'
is "$toggle.shownItems.map(i => i.on).join(',')" '"false,false"'
status docs paused
ev "$toggle.emit('clicked', 1)" >/dev/null
shows idle
is "$toggle.checked" true
status docs idle

step "Quick Settings: with every shuttle paused, the switches show it and cannot undo it"
ev "$toggle.emit('clicked', 1)" >/dev/null
shows paused
item docs "!i.on && !i.sensitive"
# A click that gets through anyway asks the daemon to resume docs, which it
# cannot while all are paused: the switch goes back to what the daemon has.
ev "$toggle._items.get('docs').toggle()" >/dev/null
item docs "!i.on"
status docs paused
ev "$toggle.emit('clicked', 1)" >/dev/null
shows idle
item docs "i.on && i.sensitive"

step "Quick Settings: a switch pauses one shuttle, and resumes it"
ev "$toggle._items.get('docs').toggle()" >/dev/null
status docs paused
item docs "!i.on"
item pics "i.on"
status pics idle
is "$toggle.checked" true
is "$toggle.subtitle" '"1 of 2 paused"'
snap quick-settings "Main.panel.statusArea.quickSettings.menu $toggle.menu"
ev "$toggle._items.get('docs').toggle()" >/dev/null
status docs idle
item docs "i.on"

step "Quick Settings: a shuttle turned off in the config has no switch"
"$shuttle" disable pics >/dev/null
item pics "i.off && i.on"
# The daemon refuses to pause a shuttle that is turned off: a request that
# gets through anyway leaves the switch as the daemon has it.
ev "$toggle._items.get('pics').toggle()" >/dev/null
sleep 0.5
item pics "i.off && i.on"
"$shuttle" enable pics >/dev/null
item pics "!i.off && i.on"

step "Quick Settings: Open shuttle opens the app"
open_from "$toggle"

step "the dropdown: All shuttles pauses every shuttle, and resumes them"
is "$indicator._all.visible && $indicator._all.state" true
row docs "r.on && r.sensitive && !r.off"
ev "$indicator._all.toggle()" >/dev/null
shows paused
status docs paused
is "$toggle.checked" false
row docs "!r.on && !r.sensitive"
is "$indicator._all.state" false
ev "$indicator._all.toggle()" >/dev/null
shows idle
status docs idle
is "$toggle.checked" true
row docs "r.on && r.sensitive"

step "the dropdown: a switch pauses one shuttle, and resumes it"
ev "$indicator._items.get('docs').toggle()" >/dev/null
status docs paused
row docs "!r.on"
row pics "r.on"
item docs "!i.on"
is "$indicator._all.state" true
snap dropdown
ev "$indicator._items.get('docs').toggle()" >/dev/null
status docs idle
row docs "r.on"

step "the dropdown: Sync now syncs"
echo "b" >"$HOME/Documents/b.txt"
ev "$indicator._items.get('docs')._button.emit('clicked', 1)" >/dev/null
for _ in $(seq 50); do [ -f "$tmp/share/docs/b.txt" ] && break; sleep 0.1; done
[ -f "$tmp/share/docs/b.txt" ] || fail "Sync now in the dropdown did not push"
shows idle

step "the dropdown: a shuttle turned off in the config has no switch"
"$shuttle" disable pics >/dev/null
row pics "r.off && r.state === 'disabled'"
"$shuttle" enable pics >/dev/null
row pics "!r.off && r.on"

step "the dropdown: Open shuttle opens the app"
open_from "$indicator"

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

step "a conflict waits for the user: held, until it is resolved"
echo "one" >"$HOME/Pictures/x.txt"
"$shuttle" sync pics >/dev/null
for _ in $(seq 50); do [ -f "$tmp/share/pics/x.txt" ] && break; sleep 0.1; done
[ -f "$tmp/share/pics/x.txt" ] || fail "the sync did not carry x.txt"
shows idle
echo "changed here" >"$HOME/Pictures/x.txt"
echo "changed there" >"$tmp/share/pics/x.txt"
touch -d '+2 seconds' "$HOME/Pictures/x.txt" "$tmp/share/pics/x.txt"
"$shuttle" sync pics >/dev/null
shows held
row pics "r.state === 'held' && r.detail.includes('1 conflict waiting for you')"
is "$toggle.subtitle" '"Waiting for you"'
snap held
gdbus call --session --dest io.github.fqazzazee.Shuttle --object-path /io/github/fqazzazee/Shuttle \
	--method io.github.fqazzazee.Shuttle1.ResolveConflict pics x.txt keep-local >/dev/null
shows idle
row pics "r.state === 'idle' && r.detail === ''"

step "a share that cannot be reached: offline"
config pics far
rows "docs,far,pics"
"$shuttle" sync far >/dev/null || true
shows offline
row far "r.state === 'offline'"
is "$toggle.subtitle" '"A share is offline"'
snap offline
config pics
rows "docs,pics"
shows idle

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
is "$toggle.checked" false
is "$toggle.shownItems.length" 0
is "$indicator._all.visible" false

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
