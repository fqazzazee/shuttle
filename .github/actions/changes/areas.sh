#!/usr/bin/env bash
# SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0
#
# Sorts the files a pull request changes into the areas CI tests, and
# writes go=, extension=, packaging=, android= and windows= to
# $GITHUB_OUTPUT. Documentation alone needs none; a file it does not know
# needs all, so a new kind of file can only make CI run more, never less.
# Not a pull request: all.
set -euo pipefail

go=false extension=false packaging=false android=false windows=false
all() { go=true extension=true packaging=true android=true windows=true; }

if [ "$EVENT" != pull_request ]; then
	all
else
	while IFS= read -r f; do
		case "$f" in
		# Documentation and repository metadata: nothing to build or test.
		*.md | LICENSE | extension/LICENSE | docs/* | .github/ISSUE_TEMPLATE/* | .github/CODEOWNERS | \
			.github/FUNDING.yml | .github/dependabot.yml | .editorconfig | .gitignore | .pre-commit-config.yaml) ;;
		extension/* | package.json | package-lock.json | eslint.config.js | test/extension/*) extension=true ;;
		# The APK's notices are built by these two as well.
		packaging/notices.sh | packaging/android-notices.py) packaging=true android=true ;;
		packaging/* | .goreleaser.yaml | test/packages/*) packaging=true ;;
		android/*) android=true ;;
		# Code built for Windows alone (the _windows file suffix): never in the
		# Android app.
		*_windows.go | *_windows_test.go) go=true windows=true ;;
		# The desktop's own packages whose suites also run on Windows, and
		# secretservice, which the shares, TUI and CLI tests import there.
		cmd/* | internal/terminal/* | internal/tui/* | internal/secrets/credman/* | internal/network/* | \
			internal/tray/* | internal/secrets/secretservice/*) go=true windows=true ;;
		# Go the Android app cannot reach, Linux's alone so far: the rest of the
		# desktop's packages, and the scripts of tests that run on Linux only.
		internal/bus/* | internal/notify/* | test/*) go=true ;;
		# The core and the daemon, whose suites also run on Windows, the packages
		# those suites import, and the modules they build with.
		internal/config/* | internal/fsys/* | internal/engine/* | internal/state/* | internal/activity/* | \
			internal/ignore/* | internal/nfs3/* | internal/daemon/* | internal/shares/* | internal/secrets/* | \
			internal/present/* | internal/pick/* | internal/notice/* | internal/dbustest/* | \
			go.mod | go.sum) go=true android=true windows=true ;;
		# The rest of the Go, the binding included, goes into the app's .aar.
		*.go | .golangci.yaml | internal/* | mobile/*) go=true android=true ;;
		# CI itself, the Makefile every job runs, and anything else: all.
		*) all ;;
		esac
	# --no-renames: a rename would list only its new path, and a file moved
	# into docs/ would then count as documentation alone.
	done < <(git diff --no-renames --name-only "$BASE" "$HEAD")
fi

{
	echo "go=$go"
	echo "extension=$extension"
	echo "packaging=$packaging"
	echo "android=$android"
	echo "windows=$windows"
} >>"$GITHUB_OUTPUT"
{
	echo "### What this run tests"
	echo
	echo "| area | runs |"
	echo "|---|---|"
	echo "| Go | $go |"
	echo "| GNOME extension | $extension |"
	echo "| packaging | $packaging |"
	echo "| Android | $android |"
	echo "| Windows | $windows |"
} >>"${GITHUB_STEP_SUMMARY:-/dev/null}"
