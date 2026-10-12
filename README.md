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

1. Terminal UI (until then: edit the config file, use the commands below)
2. NFSv3 backend, plus already-mounted paths for NFSv4-only servers
3. GNOME Shell 50 extension: indicator and Quick Settings toggle
4. Packages: RPM (Fedora COPR), DEB, extensions.gnome.org

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
notify = { completed = false }                  # errors, conflicts, completed, share

[shuttles.documents]
profile = "live"
local = "~/Documents"
remote = "nas:/docs"
```

Bandwidth limits go in a profile or a shuttle: `bandwidth = { up = "20MB/s", down = "50MB/s" }`.
The same key under `[global]` caps all shuttles together. A change to the global cap applies
from the next file, without restarting anything.

Shares can also be added in the terminal interface (`shuttle`, then `5`), which tests them and
keeps their passwords in the keyring. For a share written by hand, store its password with:

```bash
secret-tool store --label "shuttle: smb://you@nas.example/home" \
    service shuttle username "smb://you@nas.example/home"
```

Then start the service with your desktop session:

```bash
systemctl --user daemon-reload
systemctl --user enable --now shuttle
```

The service shows desktop notifications when a run fails, meets conflicts or holds back
deletions, when a run completes, and when a share is lost or comes back. Each has a switch
under `notify`, all on by default except "completed" for real-time shuttles. A run you
did not ask for is only announced when it changed something.

The service follows the config file: save a change and it applies within a second, with no
restart. Shuttles you didn't touch carry on; a changed one finishes the file it is copying,
then continues with the new settings. If an edit has a mistake, the service keeps the previous
settings, and says what is wrong in `shuttle status`, a notification and the journal. If you
point a shuttle at other folders, it starts afresh, like a first sync: nothing is deleted, and
files that differ become conflicts.

A broken config at startup stops the service with a message in `journalctl --user -u shuttle`, rather
than restarting it over and over.

## Use

Run `shuttle` on its own for the terminal interface: every shuttle live, with its progress and
transfer rates, and keys to sync (`s`), pause (`p`, or `P` for all) and disable (`d`) it. The
Conflicts tab lists what waits for you, with both copies' size and time: keep this computer's
copy (`l`), the share's (`r`) or both (`b`), and let deletions the guard held back go ahead
(`a`). The Activity tab shows what the shuttles did lately, newest first, as it happens; `f`
shows one shuttle at a time. The Shares tab adds (`n`), edits (`e`), tests (`t`) and deletes
(`x`) shares, works with the daemon stopped, and keeps passwords in the keyring, never in the
config file. The Profiles tab adds (`n`), edits (`e`), copies (`c`) and deletes (`x`) profiles: a
setting left empty is not written, and its default applies. `?` lists the keys. The shuttle
editor is still to come.

```bash
shuttle status            # one line per shuttle
shuttle sync documents    # sync now
shuttle pause [documents] # pause one shuttle, or all
shuttle resume [documents]
shuttle disable documents # turn a shuttle off, or on again with enable
shuttle activity          # what every shuttle did lately; or one: shuttle activity documents
```

The activity log keeps each file copied or deleted, each conflict and error, and a summary of
every run that did something, for 30 days or 10,000 entries per shuttle, whichever is less. It
lives next to the shuttle's state in `~/.local/state/shuttle/`.

`enable` and `disable` are saved in the config file, as `enabled = false` under the shuttle, and
change nothing else in it. Without a running daemon they edit the file directly, and the change
applies when the daemon starts.

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
