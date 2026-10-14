# shuttle

Push, pull and sync folders between SMB/NFS network shares and local storage, from a
terminal UI, with a GNOME top-bar indicator and a Quick Settings toggle.

[![Go 1.26](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Bubble Tea v2](https://img.shields.io/badge/Bubble%20Tea-v2-FF5F87)](https://github.com/charmbracelet/bubbletea)
[![Lip Gloss v2](https://img.shields.io/badge/Lip%20Gloss-v2-FF87D7)](https://github.com/charmbracelet/lipgloss)
[![Bubbles v2](https://img.shields.io/badge/Bubbles-v2-6C50FF)](https://github.com/charmbracelet/bubbles)
[![bbolt](https://img.shields.io/badge/state-bbolt-555555)](https://github.com/etcd-io/bbolt)
[![D-Bus](https://img.shields.io/badge/IPC-D--Bus-2D6DB5)](https://github.com/godbus/dbus)
[![Secret Service](https://img.shields.io/badge/passwords-Secret%20Service-4A86CF)](https://github.com/zalando/go-keyring)

[![Linux](https://img.shields.io/badge/Linux-only-FCC624?logo=linux&logoColor=black)](#install)
[![Fedora 44](https://img.shields.io/badge/Fedora-44-51A2DA?logo=fedora&logoColor=white)](https://fedoraproject.org)
[![Debian stable](https://img.shields.io/badge/Debian-stable-A81D33?logo=debian&logoColor=white)](https://www.debian.org)
[![GNOME Shell 50](https://img.shields.io/badge/GNOME%20Shell-50-4A86CF?logo=gnome&logoColor=white)](#gnome-extension)
[![systemd user service](https://img.shields.io/badge/systemd-user%20service-30D475)](https://systemd.io)
[![SMB 2 and 3](https://img.shields.io/badge/SMB-2%20%2F%203-6F42C1)](https://github.com/cloudsoda/go-smb2)
[![NFSv3](https://img.shields.io/badge/NFS-v3-6F42C1)](https://www.rfc-editor.org/rfc/rfc1813)

[![License: PolyForm Noncommercial or Internal Use](https://img.shields.io/badge/license-PolyForm%20Noncommercial%20%7C%20Internal%20Use-F5C451)](#license)
[![Extension: GPL-2.0-or-later](https://img.shields.io/badge/extension-GPL--2.0--or--later-61AFEF)](extension/LICENSE)
[![Buy me a coffee](https://img.shields.io/badge/Buy%20me%20a%20coffee-FFDD00?logo=buymeacoffee&logoColor=black)](https://buymeacoffee.com/fqazzazee)

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/screenshots/dashboard-dark.svg">
  <img alt="shuttle's terminal interface: five shuttles, two of them pushing to a NAS, under the bus and the wordmark; below, the selected one in full, its transfer rate drawn as a sparkline" src="docs/screenshots/dashboard-light.svg">
</picture>

## What it does

shuttle keeps folders on your machine and folders on a NAS or file server in step. Each
folder pair (a *shuttle*) is a one-way push, a one-way pull, or a two-way sync, and runs in
real time, on a schedule, when the share comes online, or only when you ask. Settings such
as conflict handling, deletions and file versioning come from reusable *profiles*, which a
shuttle can override one setting at a time. It talks SMB and NFSv3 directly, so it needs no
root, fstab entries or mounts. A run that would not fit on either side stops before it copies
anything, and says how much room it needed.

It is not a cloud sync client, a backup tool with deduplication, or a peer-to-peer system
like Syncthing.

## Status

Early, but usable: the sync engine, SMB and NFSv3 shares, the keyring, the background service,
the terminal interface and the GNOME Shell 50 extension work. SMB is tested against a real Samba server, NFS so far against
an in-process test server. Still to come:

1. NFS tested against the Linux kernel server: NFSv3 directly, NFSv4 through a mount
2. Packages: RPM, DEB and a tarball on GitHub Releases

### Not planned as of right now but could be an addition in the future if requested

- Cloud storage (S3, Google Drive, OneDrive, ...): use rclone
- Peer-to-peer sync between machines without a share in between: use Syncthing
- GNOME versions before 50, or other desktops' panels (the TUI and service work anywhere)

## Install

As a package from the [latest release](https://github.com/fqazzazee/shuttle/releases/latest),
for amd64 (installed and tested on Fedora 44 and Debian stable) or arm64 (built, not yet
tested):

```bash
v=0.1.0   # the release's version
# Fedora
sudo dnf install https://github.com/fqazzazee/shuttle/releases/download/v$v/shuttle_${v}_linux_amd64.rpm
# Debian, Ubuntu
curl -LO https://github.com/fqazzazee/shuttle/releases/download/v$v/shuttle_${v}_linux_amd64.deb
sudo apt install ./shuttle_${v}_linux_amd64.deb
```

Each release lists its files' SHA-256 sums in `checksums.txt`: with it beside your
downloads, `sha256sum -c --ignore-missing checksums.txt` checks them. The tarballs hold the
binary alone, with its licenses.

From source, into `~/.local/bin`, with a systemd user unit:

```bash
git clone https://github.com/fqazzazee/shuttle.git
cd shuttle && make install
```

Or as a package built from the clone:

```bash
make packages                                      # RPM, DEB and tarballs in dist/
sudo dnf install ./dist/shuttle_*_linux_amd64.rpm  # Fedora; on Debian or Ubuntu:
sudo apt install ./dist/shuttle_*_linux_amd64.deb  #   the .deb
```

A package installs the same things system-wide, the GNOME extension included (enable it
as below). Either way, the service is yours to enable.

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

An NFSv3 export needs no password:

```toml
[shares.media]
type = "nfs3"
host = "nas.example"
export = "/volume1/media"
# uid = 1000    # who the server takes you to be: your own user and group IDs if left out
# gid = 1000
```

NFSv3 trusts the network. The server believes whatever user and group IDs a client sends, so
any machine that can reach the export can read and write as anyone. Export only to the machines
that should have it.

shuttle runs as you, not as root, so it connects from a port above 1024. Linux servers refuse
that unless the export has `insecure` in `/etc/exports`; on a Synology, tick "Allow connections
from non-privileged ports". shuttle finds the server's NFS and mount services through its
portmapper (port 111); for a server whose firewall opens only those, set `port` and `mount_port`.
A server that speaks only NFSv4 has no mount service: mount it the usual way and add the mount
point as a share with `type = "path"` and `path = "/mnt/media"`.

Then start the service with your desktop session:

```bash
systemctl --user daemon-reload
systemctl --user enable --now shuttle
```

The service shows desktop notifications when a run fails, meets conflicts or holds back
deletions, when a run completes, and when a share is lost or comes back. Those that wait for
you have an "Open shuttle" button, which opens the terminal interface on its Conflicts tab. It
uses the first terminal it finds (`xdg-terminal-exec`, Ptyxis, GNOME Console, GNOME Terminal,
Konsole, …); to choose one, add `[global] terminal = "ptyxis --new-window --"` to the config:
the command that runs another in a new window. Each has a switch
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

### GNOME extension

On GNOME 50, an extension shows shuttle in the top bar and in Quick Settings: an icon for
the overall state (pulsing while it syncs, with the transfer rate beside it) and a list of
the shuttles with what each is doing; a toggle that pauses and resumes every shuttle, a switch
and a "Sync now" button for each, and "Open shuttle". The top bar's menu has the same
controls as the Quick Settings toggle. It only talks to the running service; settings stay in
the terminal interface.

The extension comes from this repository: the RPM and DEB install it for every user, and
from a checkout it installs for you alone. It is not on extensions.gnome.org.

```bash
make install-extension   # into ~/.local/share/gnome-shell/extensions
```

Then log out and back in (on Wayland, GNOME Shell finds new extensions at login), and run
`gnome-extensions enable shuttle@fqazzazee.github.io`. `make uninstall-extension` takes it
out again.

## Use

Run `shuttle` on its own for the terminal interface: every shuttle live, with its progress,
transfer rates and whether its share is online; under them, the selected one in full: its folders
and the room on each side, what it syncs, when it runs next and what its last run did. There are
keys to sync (`s`), pause (`p`, or `P` for all) and disable (`d`) it, and to add (`n`), edit (`e`)
and delete (`x`) shuttles: a few steps that browse the share's folders and yours, choose a
profile, and set what the shuttle does differently from it. On the share and on
this computer, the folder opens as a tree of what is in it, where `m` chooses to sync everything,
only what you mark, or all but what you mark, and `space` marks a file or folder; a mark made on
one side shows on the other. The
Conflicts tab lists what waits for you, with both copies' size and time: keep this computer's
copy (`l`), the share's (`r`) or both (`b`), and let deletions the guard held back go ahead
(`a`). The Activity tab shows what the shuttles did lately, newest first, as it happens; `f`
shows one shuttle at a time. The Shares tab adds (`n`), edits (`e`), tests (`t`) and deletes
(`x`) shares, works with the daemon stopped, and keeps passwords in the keyring, never in the
config file. Its FREE column shows how much room each share has (as of its last test or run) and,
under the table, this computer's. The Profiles tab adds (`n`), edits (`e`), copies (`c`) and deletes (`x`) profiles: a
setting left empty is not written, and its default applies. `?` lists the keys.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/screenshots/help-dark.svg">
  <img alt="The keys, listed in a frame over the dimmed dashboard" src="docs/screenshots/help-light.svg">
</picture>

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
make screenshots # the README's screenshots, from a scratch daemon with made-up data
```

## Contributing

See [`CONTRIBUTING.md`](CONTRIBUTING.md). Open an issue before starting anything large.

## Support

If it saved you dragging folders to the NAS by hand, you can [buy me a coffee ☕](https://buymeacoffee.com/fqazzazee).

## License

shuttle is source-available, not open source. You can use it under either of two
licenses, whichever fits you:

- **[PolyForm Noncommercial 1.0.0](https://polyformproject.org/licenses/noncommercial/1.0.0)**
  covers personal, hobby and home-lab use with no commercial purpose, plus charities,
  schools, public research and government.
- **[PolyForm Internal Use 1.0.0](https://polyformproject.org/licenses/internal-use/1.0.0)**
  covers a company running shuttle on its own systems, for its own files, at no cost.

Using shuttle to deliver work for someone else needs a commercial license. That includes
consulting, freelancing, contracting and managed services, even if you work alone. Email
fadi.qazzazee@gmail.com to arrange one.

The GNOME Shell extension in [`extension/`](extension) is free software, under the
GPL-2.0-or-later ([`extension/LICENSE`](extension/LICENSE)), as GNOME Shell's own license
asks of its extensions. See [`LICENSE`](LICENSE) for the full terms, including what the
LGPL-licensed library in shuttle's binary allows you.
