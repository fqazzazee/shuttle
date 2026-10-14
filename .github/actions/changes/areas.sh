#!/usr/bin/env bash
# SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0
#
# Sorts the files a pull request changes into the areas CI tests, and
# writes go=, extension=, packaging= and android= to $GITHUB_OUTPUT. Documentation
# alone needs none; a file it does not know needs all, so a new kind of
# file can only make CI run more, never less. Not a pull request: all.
set -euo pipefail

go=false extension=false packaging=false android=false
all() { go=true extension=true packaging=true android=true; }

if [ "$EVENT" != pull_request ]; then
	all
else
	while IFS= read -r f; do
		case "$f" in
		# Documentation and repository metadata: nothing to build or test.
		*.md | LICENSE | extension/LICENSE | docs/* | .github/ISSUE_TEMPLATE/* | .github/CODEOWNERS | \
			.github/FUNDING.yml | .github/dependabot.yml | .editorconfig | .gitignore | .pre-commit-config.yaml) ;;
		extension/* | package.json | package-lock.json | eslint.config.js | test/extension/*) extension=true ;;
		packaging/* | .goreleaser.yaml | test/packages/*) packaging=true ;;
		android/*) android=true ;;
		# Go the Android app cannot reach: the desktop's own packages, and
		# the scripts of tests that run on Linux only.
		cmd/* | internal/bus/* | internal/notify/* | internal/network/* | internal/terminal/* | internal/tui/* | \
			internal/secrets/secretservice/* | test/*) go=true ;;
		# The rest of the Go, the binding included, goes into the app's .aar.
		*.go | go.mod | go.sum | .golangci.yaml | internal/* | mobile/*) go=true android=true ;;
		# CI itself, the Makefile every job runs, and anything else: all.
		*) all ;;
		esac
	done < <(git diff --name-only "$BASE" "$HEAD")
fi

{
	echo "go=$go"
	echo "extension=$extension"
	echo "packaging=$packaging"
	echo "android=$android"
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
} >>"${GITHUB_STEP_SUMMARY:-/dev/null}"
