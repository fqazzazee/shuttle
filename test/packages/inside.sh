#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-2.0-or-later
#
# Runs in a throwaway container from run.sh: see there.
set -euo pipefail

pkg=$1
ext=/usr/share/gnome-shell/extensions/shuttle@fqazzazee.github.io
unit=/usr/lib/systemd/user/shuttle.service
desktop=/usr/share/applications/io.github.fqazzazee.Shuttle.desktop
fail() {
	echo "FAIL: $*"
	exit 1
}
# sums <dir>: every file under dir with its checksum, by path.
sums() { (cd "$1" && find . -path ./test -prune -o -type f -print | sort | xargs sha256sum); }

case $pkg in
*.rpm)
	install() { dnf -y -q install "$pkg"; }
	remove() { dnf -y -q remove shuttle; }
	;;
*.deb)
	install() { apt-get -qq update && apt-get -y -qq install "$pkg"; }
	remove() { apt-get -y -qq purge shuttle; }
	;;
esac

install >/dev/null 2>&1 || fail "the package did not install"
cd /
shuttle version
shuttle help >/dev/null
grep -qx 'ExecStart=/usr/bin/shuttle daemon' "$unit" || fail "$unit does not start /usr/bin/shuttle"
grep -qx 'Exec=/usr/bin/shuttle open' "$desktop" || fail "$desktop does not run /usr/bin/shuttle"
# The extension as it is in the repo, folders and all, without its tests.
[ "$(sums /src/extension)" = "$(sums "$ext")" ] || fail "the installed extension is not the repo's: $(sums "$ext")"
[ -f /usr/share/licenses/shuttle/LICENSE ] || [ -f /usr/share/doc/shuttle/copyright ] || fail "no licence"
echo "installed: binary, user unit, desktop file, extension, licence"

remove >/dev/null 2>&1 || fail "the package did not remove"
for p in /usr/bin/shuttle "$unit" "$desktop" "$ext"; do
	[ ! -e "$p" ] || fail "$p is still there after removal"
done
echo "removed: nothing left behind"
