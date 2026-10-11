# shuttle

Push, pull and sync folders between SMB/NFS network shares and local storage, from a
terminal UI, with a GNOME top-bar indicator and a Quick Settings toggle.

[![CI](https://github.com/fqazzazee/shuttle/actions/workflows/ci.yaml/badge.svg)](https://github.com/fqazzazee/shuttle/actions/workflows/ci.yaml)
[![Buy me a coffee](https://img.shields.io/badge/Buy%20me%20a%20coffee-FFDD00?logo=buymeacoffee&logoColor=black)](https://buymeacoffee.com/fqazzazee)

## What it does

shuttle keeps folders on your machine and folders on a NAS or file server in step. Each
folder pair (a *shuttle*) is a one-way push, a one-way pull, or a two-way sync, and runs in
real time, on a schedule, when the share comes online, or only when you ask. Settings such
as conflict handling, deletions and file versioning come from reusable *profiles*, which a
shuttle can override one setting at a time. It talks SMB and NFSv3 directly, so it needs no
root, fstab entries or mounts.

It is not a cloud sync client, a backup tool with deduplication, or a peer-to-peer system
like Syncthing.

## Status

Early development: nothing syncs yet. Planned order:

1. Sync engine, config model (profiles and shuttles), local folders
2. SMB backend, credentials in GNOME Keyring
3. Background service (`systemd --user`), D-Bus API, triggers, notifications
4. Terminal UI
5. NFSv3 backend, plus already-mounted paths for NFSv4-only servers
6. GNOME Shell 50 extension: indicator and Quick Settings toggle
7. Packages: RPM (Fedora COPR), DEB, extensions.gnome.org

### Not planned

- Cloud storage (S3, Google Drive, OneDrive, ...): use rclone
- Peer-to-peer sync between machines without a share in between: use Syncthing
- GNOME versions before 50, or other desktops' panels (the TUI and service work anywhere)

## Install

From source, into `~/.local/bin`:

```bash
git clone https://github.com/fqazzazee/shuttle.git
cd shuttle && make install
```

Or with Go:

```bash
go install github.com/fqazzazee/shuttle/cmd/shuttle@latest
```

**Requires:** Go 1.26+ to build. Developed and tested on Fedora 44; CI also builds and runs
it on Arch and Debian.

## Use

```bash
shuttle version
```

The TUI and the background service come in later phases.

## Development

```bash
make test       # tests
make lint       # gofmt check, go vet, golangci-lint
make build      # binary in bin/
```

## Contributing

See [`CONTRIBUTING.md`](CONTRIBUTING.md). Open an issue before starting anything large.

## Support

If it saved you dragging folders to the NAS by hand, you can [buy me a coffee ☕](https://buymeacoffee.com/fqazzazee).

## License

GPL-2.0-or-later — see [`LICENSE`](LICENSE).
