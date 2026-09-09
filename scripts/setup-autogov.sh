#!/usr/bin/env bash
set -euo pipefail

module='github.com/liatrio/autogov'
version='v1.4.0'
want_sum='h1:aj+yhS852iL8dKgTaoKh8PYVxKS2zznkfp6SxO6I6Ac='

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
storage_root="$root/.autogov"
bin_dir="$storage_root/bin"
destination="$bin_dir/autogov-$version"

# the verifier storage belongs to this script. Do not follow a pre-existing
# storage symlink while creating directories or publishing a verifier.
for storage_parent in "$storage_root" "$bin_dir"; do
  if [ -L "$storage_parent" ]; then
    printf 'refusing symlinked AutoGov verifier storage parent: %s\n' "$storage_parent" >&2
    exit 1
  fi
done

# a known collision needs no download or build work. link(2) below repeats
# this check atomically when another process races this early check.
if [ -e "$destination" ] || [ -L "$destination" ]; then
  printf 'refusing to overwrite existing AutoGov verifier: %s\n' "$destination" >&2
  exit 1
fi

if [ -e "$storage_root" ] && [ ! -d "$storage_root" ]; then
  printf 'AutoGov verifier storage root is not a directory: %s\n' "$storage_root" >&2
  exit 1
fi
mkdir -p "$storage_root"
if [ -L "$bin_dir" ]; then
  printf 'refusing symlinked AutoGov verifier storage parent: %s\n' "$bin_dir" >&2
  exit 1
fi
if [ -e "$bin_dir" ] && [ ! -d "$bin_dir" ]; then
  printf 'AutoGov verifier storage directory is not a directory: %s\n' "$bin_dir" >&2
  exit 1
fi
mkdir -p "$bin_dir"

staging=$(mktemp -d "$root/.setup-autogov.XXXXXX")
temporary="$staging/autogov"
modcache="$staging/modcache"
buildcache="$staging/buildcache"
cleanup() {
  # Go module caches can contain read-only extracted module directories. This
  # tree was freshly claimed by mktemp; find does not traverse symlinks, so
  # permission repair remains confined to that tree before removal.
  find "$staging" -type d -exec chmod u+rwx {} + 2>/dev/null || true
  find "$staging" -type f -exec chmod u+rw {} + 2>/dev/null || true
  rm -rf "$staging"
}
trap cleanup EXIT

# use one fresh cache for both download and build. An explicit readonly
# GOFLAGS value and an off workspace prevent ambient replacements, overlays,
# and workspace modules from changing the pinned source. GOPROXY remains the
# caller's choice.
go_isolated() {
  env -u GOWORK -u GOFLAGS \
    GOWORK=off GOFLAGS=-mod=readonly GOTOOLCHAIN=go1.26.6 \
    GOMODCACHE="$modcache" GOCACHE="$buildcache" \
    go "$@"
}

download=$(go_isolated mod download -json "$module@$version")
got_version=$(printf '%s\n' "$download" | sed -n 's/^[[:space:]]*"Version": "\([^"]*\)",$/\1/p')
got_sum=$(printf '%s\n' "$download" | sed -n 's/^[[:space:]]*"Sum": "\([^"]*\)",$/\1/p')
module_dir=$(printf '%s\n' "$download" | sed -n 's/^[[:space:]]*"Dir": "\([^"]*\)",$/\1/p')

if [ "$got_version" != "$version" ] || [ "$got_sum" != "$want_sum" ] || [ -z "$module_dir" ]; then
  printf 'AutoGov module identity mismatch: requested %s@%s, got version=%s sum=%s\n' "$module" "$version" "$got_version" "$got_sum" >&2
  exit 1
fi
if [ ! -d "$module_dir" ] || [ "$(awk '$1 == "module" { print $2; exit }' "$module_dir/go.mod")" != "$module" ]; then
  printf 'AutoGov module directory is missing or has the wrong module identity\n' >&2
  exit 1
fi
(cd "$module_dir" && go_isolated build -o "$temporary" .)
chmod 0755 "$temporary"
if ! link "$temporary" "$destination"; then
	printf 'refusing to overwrite existing AutoGov verifier: %s\n' "$destination" >&2
	exit 1
fi
printf '%s\n' "$destination"
