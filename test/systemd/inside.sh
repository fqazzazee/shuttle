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

step "install the unit the way make install does"
mkdir -p ~/.local/share/systemd/user ~/.config/shuttle
sed 's|@BINDIR@|/opt/shuttle|g' /opt/shuttle/shuttle.service.in >~/.local/share/systemd/user/shuttle.service
mkdir -p ~/m ~/rt ~/sch ~/sa "$share/m" "$share/rt" "$share/sch" # not "$share/sa": it comes online later
cat >~/.config/shuttle/config.toml <<EOF
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
EOF
echo "first" >~/sch/first
echo "waits for the share" >~/sa/x
systemctl --user daemon-reload

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

step "real-time: a local change syncs by itself"
echo hello >~/rt/hello
wait_for "the real-time sync" 20 test -f "$share/rt/hello"

step "schedule (interval 1m) and share-available (the share's folder appears)"
wait_for "the schedule's first run, at start" 10 test -f "$share/sch/first"
echo later >~/sch/later
mkdir "$share/sa"
wait_for "the share-available run (probe every 30s)" 45 test -f "$share/sa/x"
wait_for "the schedule's next run" 75 test -f "$share/sch/later"

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
[ "$(tree ~/rt)" = "$before" ] || fail "the local folder changed"
[ "$(tree "$share/rt")" = "$before" ] || fail "the share's copy differs from the local folder"
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

step "a broken config: exit 78, and no restarts"
sed -i 's/mode = "push"/mode = "both"/' ~/.config/shuttle/config.toml
systemctl --user restart shuttle 2>/dev/null && fail "started with a broken config"
restarts=$(prop NRestarts)
sleep 15 # longer than RestartSec
[ "$(prop ExecMainStatus)" = 78 ] || fail "exit status $(prop ExecMainStatus), want 78"
[ "$(prop NRestarts)" = "$restarts" ] || fail "systemd kept restarting a broken config"
[ "$(prop ActiveState)" = failed ] || fail "state $(prop ActiveState), want failed"
journalctl --user -u shuttle --no-pager | grep -q 'mode: "both" must be one of' || fail "the journal lacks the config error"
echo "failed once with 78; the journal says why"

echo
echo "PASS: Phase 3 exit condition under systemd (notifications pending)"
