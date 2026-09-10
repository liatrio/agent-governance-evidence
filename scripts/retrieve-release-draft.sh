#!/usr/bin/env bash
# Retrieve draft metadata and assets only.
set -euo pipefail

usage() { echo "usage: $0 --tag TAG --assets-dir DIR --metadata-file FILE --repository OWNER/REPO" >&2; exit 2; }
tag= assets= metadata= repository=
while [ "$#" -gt 0 ]; do case "$1" in --tag) tag=${2-}; shift 2;; --assets-dir) assets=${2-}; shift 2;; --metadata-file) metadata=${2-}; shift 2;; --repository) repository=${2-}; shift 2;; *) usage;; esac; done
test -n "$tag" && test -n "$assets" && test -n "$metadata" && test -n "$repository" || usage
[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+-alpha\.[0-9]+$ ]] || { echo "experimental tag required" >&2; exit 1; }

mkdir -p "$assets"
release=$(gh release view "$tag" -R "$repository" --json databaseId,tagName,isDraft,isPrerelease,assets)
manifest=$(mktemp "${TMPDIR:-/tmp}/agent-governance-draft-metadata.XXXXXX")
trap 'rm -f "$manifest"' EXIT

python3 - "$tag" "$release" "$manifest" <<'PY'
import json
import sys

tag, release, output = sys.argv[1:]
r = json.loads(release)
expected = {
    f"agent-governance-evidence_{tag}_linux-amd64.tar.gz",
    f"agent-governance-evidence_{tag}_darwin-arm64.tar.gz",
    f"agent-governance-evidence_{tag}_source.tar.gz",
    "SHA256SUMS",
    "build-provenance.sigstore.json",
}
if r.get("tagName") != tag or not r.get("isDraft") or not r.get("isPrerelease"):
    raise SystemExit("unexpected release state")
assets = r.get("assets", [])
normalized = {}
for asset in assets:
    name = asset.get("name")
    digest = asset.get("digest")
    if name in normalized or name not in expected or not isinstance(digest, str) or not digest.startswith("sha256:"):
        raise SystemExit("draft release metadata asset set mismatch")
    normalized[name] = digest
if set(normalized) != expected:
    raise SystemExit("draft release metadata asset set mismatch")
with open(output, "w", encoding="utf-8") as handle:
    json.dump(
        {
            "release_id": r.get("databaseId"),
            "tag": r.get("tagName"),
            "draft": r.get("isDraft"),
            "prerelease": r.get("isPrerelease"),
            "assets": [{"name": name, "digest": normalized[name]} for name in sorted(normalized)],
        },
        handle,
        indent=2,
        sort_keys=True,
    )
    handle.write("\n")
PY

gh release download "$tag" --repo "$repository" --dir "$assets" --pattern '*'

python3 - "$tag" "$assets" "$manifest" "$metadata" <<'PY'
import hashlib
import json
import os
import stat
import sys
from pathlib import Path

tag, assets_dir, manifest, metadata = sys.argv[1:]
expected = {
    f"agent-governance-evidence_{tag}_linux-amd64.tar.gz",
    f"agent-governance-evidence_{tag}_darwin-arm64.tar.gz",
    f"agent-governance-evidence_{tag}_source.tar.gz",
    "SHA256SUMS",
    "build-provenance.sigstore.json",
}
entries = list(os.scandir(assets_dir))
if len(entries) != 5 or {entry.name for entry in entries} != expected:
    raise SystemExit("draft asset set is not exact")
for entry in entries:
    mode = entry.stat(follow_symlinks=False).st_mode
    if not stat.S_ISREG(mode) or entry.is_symlink():
        raise SystemExit("draft assets must be regular files")
local = {
    path.name: "sha256:" + hashlib.sha256(path.read_bytes()).hexdigest()
    for path in Path(assets_dir).iterdir()
}
with open(manifest, encoding="utf-8") as handle:
    recorded = {asset["name"]: asset["digest"] for asset in json.load(handle)["assets"]}
if recorded != local:
    raise SystemExit("draft release metadata digests mismatch")
Path(metadata).write_text(Path(manifest).read_text(encoding="utf-8"), encoding="utf-8")
PY
