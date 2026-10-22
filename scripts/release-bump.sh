#!/bin/sh
# Map pull-request labels to the version bump they request. Prints patch, minor
# or major.
#
# Label names arrive on stdin, one per line. A change with no semver label is a
# patch: a missing label can only under-promise a version, never inflate one.
set -eu

labels=$(cat)

if printf '%s\n' "$labels" | grep -qx 'semver:major'; then
  echo major
elif printf '%s\n' "$labels" | grep -qx 'semver:minor'; then
  echo minor
else
  echo patch
fi
