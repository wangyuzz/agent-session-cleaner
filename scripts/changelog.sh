#!/usr/bin/env bash
#
# Print the CHANGELOG entry for one version.
#
# The release workflow feeds this straight into the Release page, and runs it
# before building so that a missing entry fails in ten seconds rather than
# after four platforms have been built.
#
#   scripts/changelog.sh v0.6.0
set -euo pipefail

if [ $# -ne 1 ]; then
	echo "usage: ${0##*/} <version>" >&2
	exit 2
fi

root="$(cd "$(dirname "$0")/.." && pwd)"
version="${1#v}"

# The tag, the version the binary reports and the release notes all have to
# agree, or the Release page describes something nobody can download.
built="$(sed -n 's/^const Version = "\(.*\)"$/\1/p' "$root/internal/buildinfo/buildinfo.go")"
if [ "$version" != "$built" ]; then
	echo "${0##*/}: version $version does not match the build's $built" >&2
	exit 1
fi

# Everything from this version's heading to the next one, whatever that is.
entry="$(awk -v heading="## $version" '
	$0 == heading { inside = 1; next }
	inside && /^## / { exit }
	inside { print }
' "$root/CHANGELOG.md")"

# Trim the blank lines either side of the section.
entry="$(printf '%s' "$entry" | sed -e '/./,$!d' | sed -e :a -e '/^\n*$/{$d;N;ba' -e '}')"

if [ -z "$entry" ]; then
	echo "${0##*/}: CHANGELOG.md has no usable $version section" >&2
	exit 1
fi

printf '%s\n' "$entry"
