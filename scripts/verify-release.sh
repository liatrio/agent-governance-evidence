#!/usr/bin/env bash
# Read-only verifier for exactly one downloaded five-asset prerelease.
set -euo pipefail

usage() { echo "usage: $0 --tag TAG --commit SHA --phase draft|published --assets-dir DIR [--repository OWNER/REPO] [--source-dir DIR]" >&2; exit 2; }
tag= commit= phase= assets= repository=liatrio/agent-governance-evidence source=
while [ "$#" -gt 0 ]; do case "$1" in --tag) tag=${2-}; shift 2;; --commit) commit=${2-}; shift 2;; --phase) phase=${2-}; shift 2;; --assets-dir) assets=${2-}; shift 2;; --repository) repository=${2-}; shift 2;; --source-dir) source=${2-}; shift 2;; *) usage;; esac; done
test -n "$tag" && test -n "$commit" && test -n "$assets" || usage
case "$phase" in draft|published) ;; *) usage;; esac
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
source=${source:-$root}
python3 "$root/scripts/release_guard.py" --tag "$tag" --commit "$commit" --assets-dir "$assets" --source-dir "$source"
names=("agent-governance-evidence_${tag}_linux-amd64.tar.gz" "agent-governance-evidence_${tag}_darwin-arm64.tar.gz" "agent-governance-evidence_${tag}_source.tar.gz" SHA256SUMS build-provenance.sigstore.json)
for subject in "${names[@]:0:4}"; do
  gh attestation verify "$assets/$subject" -R "$repository" --bundle "$assets/build-provenance.sigstore.json" --source-ref "refs/tags/$tag" --source-digest "$commit" --signer-workflow "$repository/.github/workflows/release.yml" --deny-self-hosted-runners
done
release=$(gh release view "$tag" -R "$repository" --json tagName,isDraft,isPrerelease,isImmutable,assets)
tag_object=$(gh api "repos/$repository/git/ref/tags/$tag" --jq .object.sha)
peeled_commit=$(gh api "repos/$repository/git/tags/$tag_object" --jq .object.sha)
python3 - "$phase" "$commit" "$tag" "$release" "$assets" "$peeled_commit" <<'PY'
import json, sys
import hashlib
from pathlib import Path
phase, commit, tag, release, assets_dir, peeled = sys.argv[1:]
r = json.loads(release)
if r.get("isDraft") != (phase == "draft") or not r.get("isPrerelease"):
    raise SystemExit("unexpected release state")
if phase == "published" and not r.get("isImmutable"):
    raise SystemExit("published release is not immutable")
if r.get("tagName") != tag or peeled != commit:
    raise SystemExit("release identity or peeled tag commit mismatch")
names = [f"agent-governance-evidence_{tag}_linux-amd64.tar.gz", f"agent-governance-evidence_{tag}_darwin-arm64.tar.gz", f"agent-governance-evidence_{tag}_source.tar.gz", "SHA256SUMS", "build-provenance.sigstore.json"]
expected = {p.name: "sha256:" + hashlib.sha256(p.read_bytes()).hexdigest()
            for p in Path(assets_dir).iterdir()}
remote_assets = r.get("assets", [])
if len(remote_assets) != 5 or len({a.get("name") for a in remote_assets}) != 5:
    raise SystemExit("release API asset set contains missing or duplicate assets")
remote = {a.get("name"): a.get("digest") for a in remote_assets}
if remote != expected or set(remote) != set(names):
    raise SystemExit("release API asset names or digests mismatch")
PY

if [ "$phase" = published ]; then
  gh release verify "$tag" -R "$repository"
  for asset in "${names[@]}"; do
    gh release verify-asset "$tag" "$assets/$asset" -R "$repository"
  done
fi

case "$(uname -s)/$(uname -m)" in
  Linux/x86_64) native=linux-amd64 ;;
  Darwin/arm64) native=darwin-arm64 ;;
  *) echo "no native archive for this host" >&2; exit 1 ;;
esac
work=$(mktemp -d "${TMPDIR:-/tmp}/agent-governance-release-verify.XXXXXX")
trap 'rm -rf "$work"' EXIT
tar -C "$work" -xzf "$assets/agent-governance-evidence_${tag}_${native}.tar.gz"
tar -C "$work" -xzf "$assets/agent-governance-evidence_${tag}_source.tar.gz"
source_dir="$work/agent-governance-evidence_${tag}"
test -d "$source_dir" || { echo "source archive root missing" >&2; exit 1; }
"$work/checkpoint" -root "$source_dir" -verify
(cd "$source_dir" && ./scripts/setup-autogov.sh)
"$work/agent-governance-demo" --autogov "$source_dir/.autogov/bin/autogov-v1.4.0" --agent-governance-evidence "$work/agent-governance-evidence" --companion "$source_dir"
