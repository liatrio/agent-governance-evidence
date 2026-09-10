#!/usr/bin/env bash
# Validate an untrusted tag event before any privileged release job can run.
set -euo pipefail

: "${ACTOR:?ACTOR is required}"
: "${RERUN_ACTOR:?RERUN_ACTOR is required}"
: "${REPOSITORY:?REPOSITORY is required}"
: "${TAG:?TAG is required}"
: "${SHA:?SHA is required}"

test "$REPOSITORY" = liatrio/agent-governance-evidence || { echo "unexpected repository" >&2; exit 1; }
test "$ACTOR" = ianhundere || { echo "unauthorized actor" >&2; exit 1; }
test "$RERUN_ACTOR" = ianhundere || { echo "unauthorized rerun actor" >&2; exit 1; }
[[ "$TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+-alpha\.[0-9]+$ ]] || { echo "experimental tag required" >&2; exit 1; }
commit=$(git rev-parse "refs/tags/$TAG^{}")
test "$commit" = "$SHA" || { echo "tag target changed" >&2; exit 1; }
git merge-base --is-ancestor "$commit" origin/main || { echo "tag commit is not on reviewed main" >&2; exit 1; }
printf '%s\n' "$commit"
