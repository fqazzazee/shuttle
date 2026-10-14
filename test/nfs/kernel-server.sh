#!/usr/bin/env bash
# SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0
#
# The Linux kernel NFS server for CI's "nfs (kernel server)" job, the second
# half of Phase 5's exit condition. It installs a system service and rewrites
# /etc/exports and /etc/nfs.conf, so it runs in CI only, never on a desktop.
#
#   kernel-server.sh v3       export /srv/nfs/v3 to this machine, over NFSv3 and v4
#   kernel-server.sh v4-only  turn NFSv3 off, export /srv/nfs/v4 instead and mount it
#                             at /mnt/nfs4 over NFSv4.2
#
# The exported folders belong to the user running the script, as a share's
# files belong to its user: the tests set up and inspect them directly.
set -euo pipefail

if [ "${CI:-}" != true ]; then
	echo "$0 changes this machine's NFS server: it runs in CI only" >&2
	exit 1
fi

# insecure: shuttle runs unprivileged, so it connects from a port above 1024,
# which a Linux export refuses by default.
export_to_me() {
	echo "$1 127.0.0.1(rw,sync,insecure,no_subtree_check)" | sudo tee /etc/exports
	sudo systemctl restart nfs-server
	sudo exportfs -v
}

case "${1:-}" in
v3)
	sudo apt-get update -q
	sudo apt-get install -y -q --no-install-recommends nfs-kernel-server
	sudo install -d -o "$(id -u)" -g "$(id -g)" /srv/nfs/v3 /srv/nfs/v4
	export_to_me /srv/nfs/v3
	rpcinfo -p 127.0.0.1 # mountd and nfs, version 3, over tcp
	;;
v4-only)
	sudo nfsconf --set nfsd vers3 n
	export_to_me /srv/nfs/v4
	sudo install -d /mnt/nfs4
	sudo mount -t nfs4 -o vers=4.2 127.0.0.1:/srv/nfs/v4 /mnt/nfs4
	findmnt /mnt/nfs4
	# No NFSv3 left; the portmapper may be, which shuttle must cope with too.
	rpcinfo -p 127.0.0.1 || true
	;;
*)
	echo "usage: $0 v3|v4-only" >&2
	exit 2
	;;
esac
