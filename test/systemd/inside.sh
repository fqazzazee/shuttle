#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-2.0-or-later
#
# Phase 3's exit condition, under a real systemd. Runs inside the container
# from run.sh, as the lingering user "tester", with the shuttle binary and
# unit template mounted at /opt/shuttle.
set -euo pipefail

uid=$(id -u)
export XDG_RUNTIME_DIR=/run/user/$uid DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/$uid/bus
shuttle=/opt/shuttle/shuttle
share=$HOME/share # a "path" share: a folder standing in for a mounted NAS

step() { echo; echo "== $*"; }
fail() {
	echo "FAIL: $*"
	echo "--- journal"
	journalctl --user -u shuttle --no-pager | tail -60
	echo "--- notifications"
	cat ~/notes 2>/dev/null || true
	exit 1
}
# wait_for <what> <seconds> <command...>: poll until the command succeeds.
wait_for() {
	local what=$1 secs=$2
	shift 2
	for ((i = 0; i < secs * 5; i++)); do
		"$@" && return 0
		sleep 0.2
	done
	fail "timed out after ${secs}s waiting for $what"
}
prop() { systemctl --user show -P "$1" shuttle; }
# tree <dir>: content hashes of every synced file, by relative path.
tree() {
	(cd "$1" && find . -type f ! -path './.shuttle-versions/*' ! -name '.shuttle-tmp-*' -print0 |
		sort -z | xargs -0 -r sha256sum)
}
temps() { find "$share/rt" -name '.shuttle-tmp-*' | wc -l; }
# note <regex>: fakenotify has shown a matching category|summary|body line.
note() { grep -qE "$1" ~/notes; }
notes() { grep -cE "$1" ~/notes || true; }

step "install the unit the way make install does"
mkdir -p ~/.local/share/systemd/user ~/.config/shuttle
sed 's|@BINDIR@|/opt/shuttle|g' /opt/shuttle/shuttle.service.in >~/.local/share/systemd/user/shuttle.service
mkdir -p ~/m ~/rt ~/sch ~/sa "$share/m" "$share/rt" "$share/sch" # not "$share/sa": it comes online later
mkdir -p ~/c ~/u "$share/c" "$share/e" "$share/q" ~/usb/u          # not ~/e or ~/q: their runs fail
# Stands in for a terminal for "Open shuttle": notes what it was asked to run.
printf '#!/bin/sh\necho "$@" >>"$HOME/terminal"\n' >~/fake-term && chmod +x ~/fake-term
cat >~/.config/shuttle/config.toml <<EOF
[global]
terminal = "$HOME/fake-term"

[shares.disk]
type = "path"
path = "$share"

[shuttles.m]
mode = "push"
local = "~/m"
remote = "disk:/m"

[shuttles.rt]
mode = "sync"
triggers = ["realtime"]
bandwidth = { up = "4MB/s" }
local = "~/rt"
remote = "disk:/rt"

[shuttles.sch]
mode = "push"
triggers = ["schedule"]
interval = "1m"
local = "~/sch"
remote = "disk:/sch"

[shuttles.sa]
mode = "push"
triggers = ["share-available"]
local = "~/sa"
remote = "disk:/sa"

[shuttles.c]
mode = "sync"
local = "~/c"
remote = "disk:/c"

[shuttles.e]
mode = "push"
local = "~/e"
remote = "disk:/e"

[shuttles.q]
mode = "push"
local = "~/q"
remote = "disk:/q"
notify = { errors = false }

[shares.usb]
type = "path"
path = "$HOME/usb"

[shuttles.u]
mode = "push"
local = "~/u"
remote = "usb:/u"
EOF
echo "first" >~/sch/first
echo "waits for the share" >~/sa/x
systemctl --user daemon-reload
# The desktop's notification server, standing in for GNOME Shell's.
systemd-run --user -q --unit=fakenotify /opt/shuttle/fakenotify ~/notes
wait_for "the notification server" 10 sh -c "busctl --user status org.freedesktop.Notifications >/dev/null 2>&1"

step "start: Type=dbus, so start returns once the bus name is taken"
systemctl --user start shuttle || fail "start"
[ "$(prop ActiveState)" = active ] || fail "not active after start"
busctl --user status io.github.fqazzazee.Shuttle >/dev/null || fail "the bus name is not taken"
"$shuttle" status
[ "$("$shuttle" status | grep -cE '^(m|rt|sch|sa) ')" = 4 ] || fail "shuttle status does not list the 4 shuttles"

step "manual: shuttle sync"
echo a >~/m/a
"$shuttle" sync m
wait_for "the manual sync" 10 test -f "$share/m/a"
wait_for "its notification" 10 note '^transfer\.complete\|m: synced\|Copied 1 file\.$'
"$shuttle" activity m | grep -q '  m  a: copied to the share$' || fail "the activity log lacks the copy"
"$shuttle" activity m | grep -q '  m  1 copied$' || fail "the activity log lacks the run"
[ "$(stat -c %a ~/.local/state/shuttle/m.activity)" = 600 ] || fail "the activity log is readable by others"

step "real-time: a local change syncs by itself"
echo hello >~/rt/hello
wait_for "the real-time sync" 20 test -f "$share/rt/hello"

step "schedule (interval 1m) and share-available (the share's folder appears)"
wait_for "the schedule's first run, at start" 10 test -f "$share/sch/first"
echo later >~/sch/later
mkdir "$share/sa"
wait_for "the share-available run (probe every 30s)" 45 test -f "$share/sa/x"
wait_for "the schedule's next run" 75 test -f "$share/sch/later"

step "notifications, each per its switch"
"$shuttle" sync e
"$shuttle" sync q
wait_for "the error notification" 10 note '^transfer\.error\|e: sync failed\|local folder .*/e: .*is the drive mounted\?\)$'
"$shuttle" sync e # fails the same way: not news
echo base >~/c/f
"$shuttle" sync c
wait_for "c's first sync" 10 test -f "$share/c/f"
echo "local edit" >~/c/f
echo "remote edit" >"$share/c/f"
touch -d '+1 minute' "$share/c/f"
"$shuttle" sync c
wait_for "the conflict notification" 10 note '^transfer\|c: 1 conflict\|f: kept both: '
mv ~/usb/u ~/usb/u.away # the "usb" share is unplugged
wait_for "the share lost notification (probe every 30s)" 45 note '^network\.disconnected\|Share usb lost\|Waiting for it: u\.$'
mv ~/usb/u.away ~/usb/u
wait_for "the share connected notification" 45 note '^network\.connected\|Share usb connected\|Can sync again: u\.$'
[ "$(notes '\|e: sync failed\|')" = 1 ] || fail "e's unchanged error was announced again"
[ "$(notes '\|q: ')" = 0 ] || fail "q has notify.errors off, but notified"
[ "$(notes '\|rt: synced\|')" = 0 ] || fail "real-time rt announced its runs (notify.completed is off by default)"
[ "$(notes '^network\.')" = 2 ] || fail "share notifications other than usb's lost and connected (at start?)"
echo "shown:"
sed 's/^/  /' ~/notes

step "kill -9 mid-run: no lost files, no false deletions"
for i in $(seq 1 60); do head -c 524288 /dev/urandom >~/rt/big-"$i"; done # 30 MB at 4 MB/s
for i in $(seq 1 500); do echo "$i" >~/rt/small-"$i"; done
before=$(tree ~/rt)
restarts=$(prop NRestarts)
wait_for "a copy in progress" 30 sh -c "[ \$(find '$share/rt' -name '.shuttle-tmp-*' | wc -l) -gt 0 ]"
systemctl --user kill --signal=KILL shuttle
echo "killed with SIGKILL while copying"
wait_for "systemd to restart it (RestartSec=10)" 30 sh -c "[ \$(systemctl --user show -P NRestarts shuttle) -gt $restarts ] && [ \$(systemctl --user show -P ActiveState shuttle) = active ]"
files() { (cd "$1" && find . -type f ! -path './.shuttle-versions/*' ! -name '.shuttle-tmp-*' | sort); }
synced() { [ "$(files ~/rt)" = "$(files "$share/rt")" ]; }
# A real-time shuttle runs at start: well within its 60 s poll interval.
wait_for "the restarted daemon to finish the sync" 30 synced
# differs <what>: fail, showing each file that is not as before on either
# side, with sizes and times, and what each side's versions folder holds.
differs() {
	local dir
	for dir in ~/rt "$share/rt"; do
		echo "in $dir, lines only before (-) or only now (+):"
		comm -3 <(echo "$before" | sort) <(tree "$dir" | sort) | sed 's/^\t/+ /; t; s/^/- /'
		comm -3 <(echo "$before" | sort) <(tree "$dir" | sort) | sed 's/.* //' | sort -u |
			while read -r f; do stat -c '  %n: %s bytes, modified %y' "$dir/$f" 2>&1; done
		echo "versions kept in $dir:"
		find "$dir/.shuttle-versions" -type f -printf '  %P %s bytes\n' 2>/dev/null
	done
	fail "$1"
}
[ "$(tree ~/rt)" = "$before" ] || differs "the local folder changed"
[ "$(tree "$share/rt")" = "$before" ] || differs "the share's copy differs from the local folder"
[ -f ~/rt/hello ] && [ -f "$share/rt/hello" ] || fail "a file synced before the crash was deleted"
echo "all $(echo "$before" | wc -l) files intact on both sides; leftover temp files from the kill: $(temps) (removed after an hour)"

step "SIGTERM: stopping finishes the file in progress"
# The kill left temp files behind, and a run may still be copying: new
# copies are told apart by name (temp names are random), not by count.
temp_names() { find "$share/rt" -name '.shuttle-tmp-*' | sort; }
old_temps=$(temp_names)
new_temps() { comm -13 <(echo "$old_temps") <(temp_names); }
copying() { [ -n "$(new_temps)" ]; }
for i in $(seq 1 10); do head -c 1048576 /dev/urandom >~/rt/stop-"$i"; done
wait_for "a copy in progress" 30 copying
start=$(date +%s)
systemctl --user stop shuttle
took=$(($(date +%s) - start))
[ -z "$(new_temps)" ] || fail "stopping left a half-written file: $(new_temps)"
echo "stopped in ${took}s, no half-written file left"
systemctl --user start shuttle
wait_for "the rest after restarting" 30 test -f "$share/rt/stop-10"

step "config edits apply without a restart"
pid=$(prop MainPID)
mkdir -p ~/n "$share/n"
echo n >~/n/one
printf '\n[shuttles.n]\nmode = "push"\nlocal = "~/n"\nremote = "disk:/n"\n' >>~/.config/shuttle/config.toml
wait_for "the added shuttle in shuttle status" 10 sh -c "\"$shuttle\" status | grep -q '^n '"
"$shuttle" sync n
wait_for "its sync" 10 test -f "$share/n/one"
cp ~/.config/shuttle/config.toml ~/good.toml
echo 'bogus = 1' >>~/.config/shuttle/config.toml
wait_for "the config error in shuttle status" 10 sh -c "\"$shuttle\" status | grep -q 'previous settings are in use'"
wait_for "the config error notification" 10 note '^transfer\.error\|shuttle: config not loaded\|.*bogus'
echo n >~/n/two
"$shuttle" sync n
wait_for "a sync under the previous settings" 10 test -f "$share/n/two"
cp ~/good.toml ~/.config/shuttle/config.toml
wait_for "the error to clear" 10 sh -c "! \"$shuttle\" status | grep -q 'previous settings'"
[ "$(prop MainPID)" = "$pid" ] || fail "the daemon restarted for a config edit"
echo "added a shuttle, survived a broken edit and its fix, all in process $pid"

step "shuttle disable and enable: saved in the config, applied without a restart"
"$shuttle" disable n
grep -q '^enabled = false$' ~/.config/shuttle/config.toml || fail "disable did not write the config"
wait_for "n disabled in shuttle status" 10 sh -c "\"$shuttle\" status | grep -qE '^n +[^ ]+ [a-z]+ +disabled'"
"$shuttle" sync n 2>/dev/null && fail "a disabled shuttle synced"
"$shuttle" enable n
grep -q '^enabled' ~/.config/shuttle/config.toml && fail "enable left the enabled key"
[ "$(sha256sum <~/good.toml)" = "$(sha256sum <~/.config/shuttle/config.toml)" ] ||
	fail "disable and enable changed more than the enabled key"
wait_for "n enabled in shuttle status" 10 sh -c "\"$shuttle\" status | grep -qE '^n +[^ ]+ [a-z]+ +idle'"
[ "$(prop MainPID)" = "$pid" ] || fail "the daemon restarted for enable or disable"
echo "disabled and enabled n, config back byte for byte"

step "Open shuttle: a conflict waiting for you opens the TUI on it, in a terminal"
mkdir -p ~/k "$share/k"
echo mine >~/k/a; echo theirs >"$share/k/a"
printf '\n[shuttles.k]\nmode = "sync"\nlocal = "~/k"\nremote = "disk:/k"\nconflicts = "ask"\n' >>~/.config/shuttle/config.toml
wait_for "k in shuttle status" 10 sh -c "\"$shuttle\" status | grep -q '^k '"
"$shuttle" sync k
wait_for "the conflict's notification, clicked" 10 note '^clicked\|k: 1 conflict\|open$'
wait_for "the terminal asked to open shuttle" 10 grep -qx '/opt/shuttle/shuttle open conflicts' ~/terminal
echo "the button ran: $(cat ~/terminal)"

step "a broken config: exit 78, and no restarts"
sed -i 's/mode = "push"/mode = "both"/' ~/.config/shuttle/config.toml
systemctl --user restart shuttle 2>/dev/null && fail "started with a broken config"
restarts=$(prop NRestarts)
sleep 15 # longer than RestartSec
[ "$(prop ExecMainStatus)" = 78 ] || fail "exit status $(prop ExecMainStatus), want 78"
[ "$(prop NRestarts)" = "$restarts" ] || fail "systemd kept restarting a broken config"
[ "$(prop ActiveState)" = failed ] || fail "state $(prop ActiveState), want failed"
# Not "journalctl | grep -q": grep stops at the first match, journalctl dies
# writing to the closed pipe, and pipefail fails the line.
journalctl --user -u shuttle --no-pager >~/journal
grep -q 'mode: "both" must be one of' ~/journal || fail "the journal lacks the config error"
echo "failed once with 78; the journal says why"

echo
echo "PASS: Phase 3 exit condition under systemd"
