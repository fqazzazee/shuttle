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
shuttle can override one setting at a time. It talks SMB directly (NFSv3 is planned), so it needs
no root, fstab entries or mounts.

It is not a cloud sync client, a backup tool with deduplication, or a peer-to-peer system
like Syncthing.

## Status

Early, but usable from the command line: the sync engine, SMB shares, the keyring and the
background service work and are tested against a real Samba server. Still to come:

1. Desktop notifications
2. Terminal UI (until then: edit the config file, use the commands below)
3. NFSv3 backend, plus already-mounted paths for NFSv4-only servers
4. GNOME Shell 50 extension: indicator and Quick Settings toggle
5. Packages: RPM (Fedora COPR), DEB, extensions.gnome.org

### Not planned

- Cloud storage (S3, Google Drive, OneDrive, ...): use rclone
- Peer-to-peer sync between machines without a share in between: use Syncthing
- GNOME versions before 50, or other desktops' panels (the TUI and service work anywhere)

## Install

From source, into `~/.local/bin`, with a systemd user unit:

```bash
git clone https://github.com/fqazzazee/shuttle.git
cd shuttle && make install
```

**Requires:** Go 1.26+ to build, systemd, and a Secret Service (GNOME Keyring) for share
passwords. Developed and tested on Fedora 44; CI also builds and runs it on Arch and Debian.

## Configure

Write `~/.config/shuttle/config.toml`. A share, a profile, and a shuttle that uses them:

```toml
[shares.nas]
type = "smb"
host = "nas.example"
share = "home"
user = "you"              # the password goes in the keyring, never here

[profiles.live]
mode = "sync"                                   # push, pull or sync
triggers = ["realtime", "share-available"]      # or "schedule" with interval/at
versioning = { kind = "trash", keep = "30d" }

[shuttles.documents]
profile = "live"
local = "~/Documents"
remote = "nas:/docs"
```

Store the share's password in the keyring (until the TUI does it for you):

```bash
secret-tool store --label "shuttle: smb://you@nas.example/home" \
    service shuttle username "smb://you@nas.example/home"
```

Then start the service with your desktop session:

```bash
systemctl --user daemon-reload
systemctl --user enable --now shuttle
```

A broken config stops the service with a message in `journalctl --user -u shuttle`, rather
than restarting it over and over.

## Use

```bash
shuttle status            # one line per shuttle
shuttle sync documents    # sync now
shuttle pause [documents] # pause one shuttle, or all
shuttle resume [documents]
```

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
