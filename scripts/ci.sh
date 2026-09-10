#!/usr/bin/env bash
# Run the unprivileged Linux acceptance suite from an isolated checkout.
set -euo pipefail

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
test "$(uname -s)" = Linux || { echo "ci.sh requires Linux" >&2; exit 1; }
test "$(uname -m)" = x86_64 || { echo "ci.sh requires linux/amd64" >&2; exit 1; }
python3 - <<'PY'
import sys
if sys.version_info[:2] != (3, 13):
    raise SystemExit("ci.sh requires Python 3.13")
PY

work=$(mktemp -d "${TMPDIR:-/tmp}/agent-governance-ci.XXXXXX")
cleanup() {
  find "$work" -type d -exec chmod u+rwx {} + 2>/dev/null || true
  find "$work" -type f -exec chmod u+rw {} + 2>/dev/null || true
  rm -rf "$work"
}
trap cleanup EXIT
git -C "$root" diff --quiet && git -C "$root" diff --cached --quiet && test -z "$(git -C "$root" status --porcelain --untracked-files=all)" || {
  echo "ci.sh refuses a dirty source checkout" >&2; exit 1;
}
git clone --quiet --no-local "$root" "$work/source"
cd "$work/source"
export GOTOOLCHAIN=go1.26.6
export GOWORK=off
export GOPATH="$work/gopath"
export GOMODCACHE="$work/modcache"
export GOCACHE="$work/buildcache"
export XDG_CACHE_HOME="$work/cache"

./scripts/setup-autogov.sh
export AUTOGOV_BINARY="$PWD/.autogov/bin/autogov-v1.4.0"
go mod verify
cp go.mod "$work/go.mod"; cp go.sum "$work/go.sum"
go mod tidy
cmp -s go.mod "$work/go.mod" && cmp -s go.sum "$work/go.sum" || {
  echo "go mod tidy changed tracked module files" >&2; exit 1;
}
go vet ./...
go test -count=1 -v ./... | tee "$work/test.log"
python3 ./scripts/check-go-test-log.py "$work/test.log"
go build -o bin/agent-governance-evidence ./cmd/agent-governance-evidence
go build -o bin/agent-governance-demo ./cmd/demo
go build -o bin/checkpoint ./cmd/checkpoint
bin/checkpoint -root . -verify
bin/agent-governance-demo --autogov "$AUTOGOV_BINARY" --agent-governance-evidence bin/agent-governance-evidence --companion .
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 run
git diff --exit-code -- adapters fixtures policy/agent_governance.rego internal/evidence/schemas checkpoint.sha256.json LICENSE
