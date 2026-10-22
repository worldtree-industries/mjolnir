#!/usr/bin/env bash
# Decide whether a commit releases, and at which version.
#
# This lives here rather than inline in release.yml so the decision can be run
# anywhere: the push workflow, a workflow_dispatch dry run, the pull request
# dry run, and a laptop. The first version lived only in YAML and could be
# exercised only by merging, which is how a label endpoint that has never
# existed survived two green pull requests and failed two releases.
#
#   release-plan.sh                 decide for HEAD (what a push would release)
#   release-plan.sh --pr 123        decide what pull request 123 would release
#   release-plan.sh --head <rev>    decide for a revision other than HEAD
#
# Writes skip=, bump= and tag= to $GITHUB_OUTPUT when that is set; prints the
# reasoning to stdout either way.

set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
pr=
pr_mode=
head=HEAD

usage() {
  sed -n '2,/^$/p' "$0" | sed 's/^# \{0,1\}//'
}

while [ $# -gt 0 ]; do
  case "$1" in
    --pr)
      pr=${2:?--pr needs a pull request number}
      pr_mode=1
      shift 2
      ;;
    --head)
      head=${2:?--head needs a revision}
      shift 2
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

emit() {
  [ -n "${GITHUB_OUTPUT:-}" ] || return 0
  printf '%s\n' "$1" >>"$GITHUB_OUTPUT"
}

skip() {
  emit "skip=1"
  echo "$1"
  exit 0
}

subject="merged PR"
if [ -z "$pr_mode" ]; then
  # Only a merge releases. A direct push to main is still verified, but it
  # never moves the version.
  if [ "$(git rev-list --parents -n 1 "$head" | wc -w | tr -d ' ')" -lt 3 ]; then
    skip "HEAD is not a merge commit; ignoring this push (no release)."
  fi

  # The pull request number is in the merge commit's own subject, so finding it
  # costs no API call.
  pr=$(git log -1 --format=%s "$head" | sed -n 's/^Merge pull request #\([0-9][0-9]*\) .*/\1/p')
  if [ -z "$pr" ]; then
    echo "HEAD is a merge commit, but its subject does not name a pull request:" >&2
    git log -1 --format='  %s' "$head" >&2
    echo "Tag this one by hand, or restore GitHub's default merge message." >&2
    exit 1
  fi

  # Checked before the guard on purpose: a re-run would otherwise bump from the
  # tag already sitting on HEAD and demand the next major.
  tagged=$(git tag --points-at "$head" | tr '\n' ' ' | sed 's/ $//')
  if [ -n "$tagged" ]; then
    skip "HEAD is already tagged (${tagged}); nothing to do."
  fi
else
  subject="pull request"
fi
echo "${subject} #${pr}"

repo=${GITHUB_REPOSITORY:-$(gh repo view --json nameWithOwner -q .nameWithOwner)}

# Labels live on the issue form of the pull request: there is no
# /pulls/{n}/labels endpoint, and asking for one 404s on every request.
# Retried because this is the only fact that needs the API at all.
labels=
read_ok=
for attempt in 1 2 3; do
  if labels=$(gh api "repos/${repo}/issues/${pr}/labels" --jq '.[].name' 2>&1); then
    read_ok=1
    break
  fi
  echo "reading labels for PR #${pr} failed (attempt ${attempt}): ${labels}" >&2
  sleep 3
done
if [ -z "$read_ok" ]; then
  echo "could not read the labels of PR #${pr}; refusing to guess a version" >&2
  exit 1
fi

bump=$(printf '%s\n' "$labels" | "$here/release-bump.sh")
echo "requested bump: ${bump}"
emit "bump=${bump}"

"$here/check-major-version.sh" "$bump"

next=$("$here/next-version.sh" "$bump")
echo "next release: ${next}"
emit "skip=0"
emit "tag=${next}"
