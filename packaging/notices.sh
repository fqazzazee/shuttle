#!/usr/bin/env bash
# SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0
#
# Writes THIRD_PARTY_NOTICES to stdout: the license and notice files of
# every module linked into shuttle's binary, for each architecture
# released, and of Go itself, whose runtime and standard library are
# linked too; then the GPL-3.0, which the LGPL-3.0 of one of them asks to
# go with it. A module without a license file stops it: notices that miss
# one are not notices.
#
# notices.sh android: the same for the Go core in the Android app (./mobile
# for android/arm64, with gomobile's runtime); android-notices.py adds the
# app's Java and Kotlin libraries after it.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"

for=${1:-desktop}
format='{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}} {{.Dir}}{{end}}{{end}}'

# module path, version and folder, once each, for every architecture.
modules() {
	if [ "$for" = android ]; then
		# gomobile bind links its runtime (bind/seq, bind/java) with ./mobile.
		CGO_ENABLED=1 GOOS=android GOARCH=arm64 go list -deps -f "$format" \
			./mobile golang.org/x/mobile/bind/seq golang.org/x/mobile/bind/java
	else
		for arch in amd64 arm64; do
			CGO_ENABLED=0 GOOS=linux GOARCH=$arch go list -deps -f "$format" ./cmd/shuttle
		done
	fi | sort -u
}

# heading <title>: a section's title, under a rule.
heading() { printf '\n%s\n== %s\n\n' "$(printf '=%.0s' {1..78})" "$1"; }

# section <title> <folder>: the folder's license, notice and patent files.
section() {
	local files
	files=$(find "$2" -maxdepth 1 -type f \( -iname 'LICEN[CS]E*' -o -iname 'COPYING*' -o -iname 'NOTICE*' -o -iname 'PATENTS*' \) | sort)
	if ! grep -qiE '/(LICEN[CS]E|COPYING)[^/]*$' <<<"$files"; then
		echo "notices.sh: no license file for $1 in $2" >&2
		exit 1
	fi
	heading "$1"
	for f in $files; do
		[ "$(wc -l <<<"$files")" -gt 1 ] && printf -- '-- %s\n\n' "$(basename "$f")"
		cat "$f"
		printf '\n'
	done
}

if [ "$for" = android ]; then
	cat <<'EOF'
Third-party notices for shuttle's Android app

The app includes the software below, each under the license that follows
its name: first the Go modules of its core, then the Java and Kotlin
libraries of its screens. shuttle itself is under the terms in LICENSE, in
shuttle's repository.
EOF
else
	cat <<'EOF'
Third-party notices for shuttle

shuttle's binary includes the software below, each under the license that
follows its name. shuttle itself is under the terms in LICENSE; its GNOME
Shell extension is under the GPL-2.0-or-later, in a license file of its own.
EOF
fi

# Go's own license is in its root, but where a distribution moved it, as
# Fedora's golang package does.
golicense=$(go env GOROOT)
[ -f "$golicense/LICENSE" ] || golicense=/usr/share/licenses/golang
section "Go $(go env GOVERSION | sed 's/[ -]X:.*//'): the runtime and standard library" "$golicense"
while read -r path version dir; do
	section "$path $version" "$dir"
done < <(modules)

heading "The GNU General Public License, version 3: github.com/cloudsoda/sddl is
under the GNU Lesser General Public License, version 3, above, which is the
GPL-3.0 with additional permissions"
cat packaging/licenses/GPL-3.0.txt
