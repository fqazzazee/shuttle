# Security policy — shuttle

## Supported versions

Only the latest release gets security fixes.

| Version | Supported |
|---|---|
| latest | yes |
| older | no |

## Reporting a vulnerability

Do not open a public issue.

Use [private vulnerability reporting](https://github.com/fqazzazee/shuttle/security/advisories/new),
or email fadi.qazzazee@gmail.com with the details and, if you have one, a proof of concept.

Expect an acknowledgment within 72 hours and an assessment within a week. If a fix is
warranted, you will get the timeline and credit in the release notes unless you would
rather not be named.

## Scope

**In scope:** the code in this repository, its default configuration, and its release
artifacts.

**Out of scope:** vulnerabilities in dependencies without a working exploit path through
shuttle, and anything that requires an attacker to already have the privileges the issue
would grant (for example, write access to your home directory).

## What matters most here

shuttle holds credentials for file servers and can overwrite and delete files on both
sides of a sync. Reports in these areas are taken seriously even when they look minor:

- **Credential exposure:** share passwords leaking into logs, config files, the D-Bus API,
  crash output or process arguments.
- **Path handling:** a file or folder name on a share that makes shuttle read or write
  outside the configured folders (`..`, absolute paths, symlinks, Windows reserved names).
- **Unintended deletion or overwrite:** anything that makes a mirror or two-way sync delete
  or replace files it should not, including after a crash, a share going offline mid-run,
  or a corrupted state database.
- **The D-Bus interface:** another local user or sandboxed app controlling the service.
