#!/usr/bin/env bash
# SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0
#
# Runs inside.sh in a throwaway container with systemd as PID 1, so the
# systemd unit is tested for real without touching the host's own systemd.
# PODMAN can be e.g. "sudo podman" where rootless systemd containers fail.
set -euo pipefail

podman=${PODMAN:-podman}
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
name=shuttle-systemd-test

tmp=$(mktemp -d)
cleanup() {
	$podman rm -f "$name" >/dev/null 2>&1 || true
	rm -rf "$tmp"
}
trap cleanup EXIT

(cd "$root" && CGO_ENABLED=0 go build -trimpath -o "$tmp/shuttle" ./cmd/shuttle &&
	CGO_ENABLED=0 go build -trimpath -o "$tmp/fakenotify" ./test/systemd/fakenotify)
cp "$root/packaging/systemd/shuttle.service.in" "$here/inside.sh" "$tmp/"
chmod -R a+rX "$tmp"

$podman build -q -t shuttle-test-systemd -f "$here/Containerfile" "$here" >/dev/null
$podman rm -f "$name" >/dev/null 2>&1 || true
$podman run -d --name "$name" --systemd=always -v "$tmp:/opt/shuttle:ro,z" shuttle-test-systemd >/dev/null

for _ in $(seq 60); do
	[ "$($podman exec "$name" systemctl is-active user@1000.service 2>/dev/null)" = active ] && break
	sleep 0.5
done
$podman exec -u tester "$name" bash /opt/shuttle/inside.sh
