# Contributing to shuttle

## Before you write code

Open an issue for anything beyond a typo fix. A rejected PR wastes more of your time than
a rejected issue. Check the **Status** and **Not planned** sections of the
[README](README.md) too — work that belongs to a later phase will be asked to wait.

## Setup

```bash
git clone https://github.com/fqazzazee/shuttle.git
cd shuttle
pre-commit install   # optional; CI runs the same checks
make test
```

You need Go 1.26+. If the tests do not pass on a clean checkout, that is a bug — please
report it.

## Pull requests

- One concern per PR. Two unrelated fixes are two PRs.
- Tests come with the change. Sync tests run against temporary directories and in-process
  test servers, never against a real share or your home directory.
- Run `make lint` and `make test` before pushing. For the GNOME extension (`extension/`),
  also `make lint-extension` (needs node and npm) and `make test-extension` (needs GNOME
  Shell 50 and gjs; it runs a headless shell on a private session bus, never yours).
- Describe what changes for a user, not what changed in the code — the diff already says
  that.
- Match the existing style. Do not reformat files you did not otherwise touch.

Every check on the PR must be green before review. If a check is flaky, say so rather than
re-running it until it passes.

## Commits

`type(scope): summary` — feat, fix, docs, refactor, test, chore, ci, deps, perf, build.
Summary in the imperative, under 72 characters. PRs are squash-merged, so the PR title
becomes the commit on `main` and should follow the same format.

## Reporting bugs

Include the version, the platform, what you ran, what happened, and what you expected.
A reproduction case is worth more than a description of one.

Security issues do not go in the issue tracker — see [`SECURITY.md`](SECURITY.md).
