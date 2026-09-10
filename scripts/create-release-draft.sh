#!/usr/bin/env bash
# Create a draft only from the exact locally validated five-asset set.
set -euo pipefail

usage() { echo "usage: $0 --tag TAG --assets-dir DIR --repository OWNER/REPO" >&2; exit 2; }
tag= assets= repository=
while [ "$#" -gt 0 ]; do case "$1" in --tag) tag=${2-}; shift 2;; --assets-dir) assets=${2-}; shift 2;; --repository) repository=${2-}; shift 2;; *) usage;; esac; done
test -n "$tag" && test -n "$assets" && test -n "$repository" || usage
[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+-alpha\.[0-9]+$ ]] || { echo "experimental tag required" >&2; exit 1; }

python3 - "$tag" "$assets" <<'PY'
import os
import stat
import sys

tag, directory = sys.argv[1:]
expected = {
    f"agent-governance-evidence_{tag}_linux-amd64.tar.gz",
    f"agent-governance-evidence_{tag}_darwin-arm64.tar.gz",
    f"agent-governance-evidence_{tag}_source.tar.gz",
    "SHA256SUMS",
    "build-provenance.sigstore.json",
}
entries = list(os.scandir(directory))
if len(entries) != 5 or {entry.name for entry in entries} != expected:
    raise SystemExit("draft asset set is not exact")
for entry in entries:
    mode = entry.stat(follow_symlinks=False).st_mode
    if not stat.S_ISREG(mode) or entry.is_symlink():
        raise SystemExit("draft assets must be regular files")
PY

lookup=$(mktemp "${TMPDIR:-/tmp}/agent-governance-release-lookup.XXXXXX")
trap 'rm -f "$lookup"' EXIT
if gh api --include "repos/$repository/releases/tags/$tag" >"$lookup" 2>&1; then
  echo "release already exists" >&2
  exit 1
fi
grep -Eq '^HTTP/[0-9.]+ 404([[:space:]]|$)' "$lookup" || {
  echo "release lookup failed without a confirmed 404" >&2
  cat "$lookup" >&2
  exit 1
}

gh release create "$tag" \
  "$assets/agent-governance-evidence_${tag}_linux-amd64.tar.gz" \
  "$assets/agent-governance-evidence_${tag}_darwin-arm64.tar.gz" \
  "$assets/agent-governance-evidence_${tag}_source.tar.gz" \
  "$assets/SHA256SUMS" \
  "$assets/build-provenance.sigstore.json" \
  --repo "$repository" --draft --prerelease --verify-tag --title "$tag"
