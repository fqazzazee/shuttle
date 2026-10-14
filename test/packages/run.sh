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

# check <image> <package>
check() {
	printf '\n== %s: %s\n' "$1" "$(basename "$2")"
	$podman run --rm -v "$root:/src:ro,z" "$1" bash /src/test/packages/inside.sh "/src/dist/$(basename "$2")"
}
check registry.fedoraproject.org/fedora:44 "${rpms[0]}"
check docker.io/library/debian:stable-slim "${debs[0]}"

printf '\nPASS: the RPM and the DEB install, carry everything, and remove cleanly\n'
