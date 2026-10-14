#!/usr/bin/env bash
# SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0
#
# Installs the amd64 packages `make packages` built in throwaway containers,
# the RPM on Fedora 44 and the DEB on Debian, checks that each puts what
# `make install` and `make install-extension` would where the system looks for
# it, and that removing it takes all of it away again. Nothing is installed
# on this machine. PODMAN="sudo podman" where rootless fails.
set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
podman=${PODMAN:-podman}
shopt -s nullglob
rpms=("$root"/dist/shuttle_*_linux_amd64.rpm)
debs=("$root"/dist/shuttle_*_linux_amd64.deb)
if [ ${#rpms[@]} -ne 1 ] || [ ${#debs[@]} -ne 1 ]; then
	echo "want one amd64 RPM and one DEB in dist/: run make packages first"
	exit 1
fi

# The notices name every module built into each binary, as the binary
# itself lists them, and every tarball carries them.
notices=$root/build/package/THIRD_PARTY_NOTICES
bins=("$root"/dist/shuttle_linux_*/shuttle)
[ ${#bins[@]} -ge 2 ] || { echo "want the amd64 and arm64 binaries in dist/"; exit 1; }
for bin in "${bins[@]}"; do
	while read -r kind path version _; do
		[ "$kind" = dep ] || continue
		grep -qxF "== $path $version" "$notices" || { echo "FAIL: $path $version, in $bin, is not in the notices"; exit 1; }
	done < <(go version -m "$bin")
done
for tarball in "$root"/dist/shuttle_*.tar.gz; do
	# grep reads it all: -q would stop early, and pipefail take tar's SIGPIPE for a failure.
	tar tzf "$tarball" | grep -x THIRD_PARTY_NOTICES >/dev/null || { echo "FAIL: $tarball has no THIRD_PARTY_NOTICES"; exit 1; }
done
echo "notices: every module of ${#bins[@]} binaries, in every tarball"

# check <image> <package>
check() {
	printf '\n== %s: %s\n' "$1" "$(basename "$2")"
	$podman run --rm -v "$root:/src:ro,z" "$1" bash /src/test/packages/inside.sh "/src/dist/$(basename "$2")"
}
check registry.fedoraproject.org/fedora:44 "${rpms[0]}"
check docker.io/library/debian:stable-slim "${debs[0]}"

printf '\nPASS: the RPM and the DEB install, carry everything, and remove cleanly\n'
