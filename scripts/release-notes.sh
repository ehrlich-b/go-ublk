#!/usr/bin/env bash
# Print the changelog section for one version from the docs site's releases
# page, for a GitHub release body:  scripts/release-notes.sh v0.2.0
# The section starts at "## v0.2.0" and ends before the next "## " heading.
set -euo pipefail
version=${1:?usage: release-notes.sh vX.Y.Z}
page=$(dirname "$0")/../site/content/go-ublk/releases.md
awk -v v="$version" '
	$0 ~ "^## " v "([ (]|$)" { on = 1; next }
	on && /^## / { exit }
	on { print }
' "$page" | sed -e '/./,$!d' | sed -e :a -e '/^\n*$/{$d;N;ba' -e '}' \
	| sed -e 's#](/#](https://ublk.ehrlich.dev/#g'
