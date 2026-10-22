#!/bin/sh
# Guard the go.mod module path against the major version a bump would publish.
#
# Go modules require the /vN suffix on the module path for every major version
# N >= 2; a vN tag without it is inert, because `go list -m -versions` never
# lists it (ADR-0005). v0 and v1 take no suffix. The guard itself is ADR-0006.
#
# usage: scripts/check-major-version.sh <patch|minor|major>
set -eu

bump=${1:?usage: scripts/check-major-version.sh <patch|minor|major>}
here=$(dirname "$0")

next=$("$here/next-version.sh" "$bump")
next_major=${next#v}
next_major=${next_major%%.*}

module=$(awk '$1 == "module" {print $2; exit}' go.mod)
if [ -z "$module" ]; then
  echo "no module directive in go.mod" >&2
  exit 1
fi

expected=
if [ "$next_major" -ge 2 ]; then
  expected="/v$next_major"
fi

actual=$(printf '%s\n' "$module" | sed -n 's#.*/\(v[0-9][0-9]*\)$#/\1#p')

if [ "$actual" != "$expected" ]; then
  echo "module path does not match the requested release" >&2
  echo "  requested bump:  $bump" >&2
  echo "  next version:    $next" >&2
  echo "  module path:     $module" >&2
  echo "  expected suffix: ${expected:-<none>}" >&2
  echo "  actual suffix:   ${actual:-<none>}" >&2
  if [ "$next_major" -ge 2 ]; then
    echo "A v$next_major release needs the module path to end in $expected," >&2
    echo "and every import of this module to be updated to match." >&2
  else
    echo "The module path carries a /vN suffix but this bump stays on v$next_major." >&2
  fi
  exit 1
fi

echo "module path ok: $module (next release $next)"
