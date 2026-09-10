#!/usr/bin/env bash
# Build one native release archive and the deterministic source archive.
set -euo pipefail

usage() { echo "usage: $0 --tag vX.Y.Z-alpha.N --output DIR" >&2; exit 2; }
tag= output=
while [ "$#" -gt 0 ]; do case "$1" in --tag) tag=${2-}; shift 2;; --output) output=${2-}; shift 2;; *) usage;; esac; done
test -n "$tag" && test -n "$output" || usage
[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+-alpha\.[0-9]+$ ]] || { echo "experimental tag required" >&2; exit 1; }
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"
git diff --quiet && git diff --cached --quiet && test -z "$(git status --porcelain --untracked-files=all)" || { echo "refusing dirty source" >&2; exit 1; }
git rev-parse -q --verify "refs/tags/$tag^{}" >/dev/null || { echo "missing annotated tag $tag" >&2; exit 1; }
commit=$(git rev-parse "refs/tags/$tag^{}")
test "$commit" = "$(git rev-parse HEAD)" || { echo "tag does not name HEAD" >&2; exit 1; }
test ! -e "$output" && test ! -L "$output" || { echo "refusing existing output directory" >&2; exit 1; }
case "$(uname -s)/$(uname -m)" in Linux/x86_64) platform=linux-amd64; expected_goos=linux; expected_goarch=amd64;; Darwin/arm64) platform=darwin-arm64; expected_goos=darwin; expected_goarch=arm64;; *) echo "unsupported native release platform" >&2; exit 1;; esac
test -z "${GOOS-}" && test -z "${GOARCH-}" || { echo "refusing ambient Go cross-compilation settings" >&2; exit 1; }
export GOTOOLCHAIN=go1.26.6 GOWORK=off
test "$(go env GOOS)" = "$expected_goos" && test "$(go env GOARCH)" = "$expected_goarch" || { echo "Go target is not native" >&2; exit 1; }

output_parent=$(CDPATH= cd -- "$(dirname -- "$output")" && pwd)
output_name=$(basename -- "$output")
work=$(mktemp -d "$output_parent/.${output_name}.tmp.XXXXXX")
cleanup() {
  find "$work" -type d -exec chmod u+rwx {} + 2>/dev/null || true
  find "$work" -type f -exec chmod u+rw {} + 2>/dev/null || true
  rm -rf "$work"
  if [ "${output_claimed:-false}" = true ]; then
    find "$output" -type d -exec chmod u+rwx {} + 2>/dev/null || true
    find "$output" -type f -exec chmod u+rw {} + 2>/dev/null || true
    rm -rf "$output"
  fi
}
trap cleanup EXIT
mkdir "$work/stage" "$work/unpacked"

go build -o "$work/stage/agent-governance-evidence" ./cmd/agent-governance-evidence
go build -o "$work/stage/agent-governance-demo" ./cmd/demo
go build -o "$work/stage/checkpoint" ./cmd/checkpoint
cp LICENSE "$work/stage/LICENSE"
chmod 0755 "$work/stage/agent-governance-evidence" "$work/stage/agent-governance-demo" "$work/stage/checkpoint"
chmod 0644 "$work/stage/LICENSE"

platform_archive="$work/agent-governance-evidence_${tag}_${platform}.tar.gz"
source_archive="$work/agent-governance-evidence_${tag}_source.tar.gz"
tar -C "$work/stage" -czf "$platform_archive" agent-governance-evidence agent-governance-demo checkpoint LICENSE
git -c tar.umask=022 archive --format=tar --prefix="agent-governance-evidence_${tag}/" "$commit" | gzip -n > "$source_archive"

tar -C "$work/unpacked" -xzf "$platform_archive"
tar -C "$work/unpacked" -xzf "$source_archive"
unpacked_source="$work/unpacked/agent-governance-evidence_${tag}"
test -d "$unpacked_source" || { echo "unpacked source archive root missing" >&2; exit 1; }
(cd "$unpacked_source" && ./scripts/setup-autogov.sh)
"$work/unpacked/checkpoint" -root "$unpacked_source" -verify
"$work/unpacked/agent-governance-demo" \
  --autogov "$unpacked_source/.autogov/bin/autogov-v1.4.0" \
  --agent-governance-evidence "$work/unpacked/agent-governance-evidence" \
  --companion "$unpacked_source"

rm -rf "$work/stage" "$work/unpacked"
if ! mkdir "$output" 2>/dev/null; then
  echo "refusing output collision" >&2
  exit 1
fi
output_claimed=true
mv "$platform_archive" "$source_archive" "$output/"
rmdir "$work"
output_claimed=false
trap - EXIT
