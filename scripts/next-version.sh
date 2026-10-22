#!/bin/sh
# Print the release tag a bump would cut, based on the highest tag reachable
# from HEAD. Tags on abandoned branches are ignored on purpose: "previous
# release" means "in this history", not "highest tag in the repository".
set -eu

bump=${1:?usage: scripts/next-version.sh <patch|minor|major>}

tag=$(git tag --merged HEAD --sort=-v:refname | head -1)
if [ -n "$tag" ]; then
  version=${tag#v}
else
  version=0.0.0
fi

major=${version%%.*}
rest=${version#*.}
minor=${rest%%.*}
patch=${rest#*.}
patch=${patch%%[!0-9]*}
patch=${patch:-0}

case "$bump" in
  patch) patch=$((patch + 1)) ;;
  minor)
    minor=$((minor + 1))
    patch=0
    ;;
  major)
    major=$((major + 1))
    minor=0
    patch=0
    ;;
  *)
    echo "unknown bump '$bump': expected patch, minor or major" >&2
    exit 2
    ;;
esac

printf 'v%s.%s.%s\n' "$major" "$minor" "$patch"
