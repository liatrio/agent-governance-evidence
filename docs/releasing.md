# Releasing

Only Ian Hundere cuts releases. Before tagging, record green hosted
acceptance checks for the exact merged main SHA, then read back private
vulnerability reporting, immutable releases, and no-force/no-delete main
protection. Establish and read back two active destination tag rulesets for
`refs/tags/v*`: a creation-only rule, bypassed only by `User:138915`
(`ianhundere`) in `always` mode; and a separate update-and-deletion rule with
no bypass. An administrator can alter those rulesets, so that administrator is
the documented trust boundary; this sole-maintainer process has zero
independent approvals.

For an experimental tag such as v0.1.0-alpha.1, the release workflow first
builds native Linux amd64 and macOS arm64 archives, creates SHA256SUMS, signs
the four build subjects, and creates a draft prerelease. Before any local
verification, check out the exact approved release commit in a detached
worktree; do not let moving `main` substitute for `$commit`:

```bash
verify_root=$(mktemp -d "${TMPDIR:-/tmp}/agent-governance-verify.XXXXXX")
git fetch origin main
git worktree add --detach "$verify_root" "$commit"
cd "$verify_root"
test "$(git rev-parse HEAD)" = "$commit"
```

For draft verification, use the exact bundle produced by the privileged draft
retriever: five assets in `assets/` plus `release-metadata.json`. Reuse the
recorded alpha.3 retrieval evidence; do not replace it with a fresh `main`
checkout or a new ad hoc download:

```bash
draft_bundle=/path/from-privileged-draft-retriever
assets="$draft_bundle/assets"
metadata="$draft_bundle/release-metadata.json"
scripts/verify-release.sh --tag "$tag" --commit "$commit" --phase draft \
  --assets-dir "$assets" --metadata-file "$metadata"
gh workflow run verify-release.yml -R liatrio/agent-governance-evidence \
  --ref "$tag" \
  -f tag="$tag" -f commit="$commit" -f phase=draft
```

The dispatched draft verification workflow uses a same-run split: the
retriever job has `contents: write` so it can read live draft release
identity/state and download the five assets. The native verifier jobs have
`contents: read` only, receive the assets plus metadata artifact, and never
receive credentials. That narrows exposure, but the retriever remains the
publication-capable residual boundary. Check that the workflow revision, run
commit, and both native jobs match `$commit`.

Immediately before publication, re-read the live release and compare it
against the verified snapshot: release identity/state, these exact five asset
names and API digests, the tag's peeled commit, immutable-release enablement,
and both `refs/tags/v*` rulesets.

- `agent-governance-evidence_${tag}_linux-amd64.tar.gz`
- `agent-governance-evidence_${tag}_darwin-arm64.tar.gz`
- `agent-governance-evidence_${tag}_source.tar.gz`
- `SHA256SUMS`
- `build-provenance.sigstore.json`

Any mismatch stops publication. No draft, source asset, or tag is overwritten:
a collision or defect requires a new prerelease.

```bash
gh release edit "$tag" -R liatrio/agent-governance-evidence --draft=false
```

After publication, require immutable: true, gh release verify, and gh release
verify-asset for all five assets. Repeat provenance and native verification in
published mode from newly downloaded assets:

```bash
published_assets=$(mktemp -d "${TMPDIR:-/tmp}/agent-governance-published.XXXXXX")
gh release download "$tag" -R liatrio/agent-governance-evidence \
  --dir "$published_assets" --pattern '*'
gh release verify "$tag" -R liatrio/agent-governance-evidence
for asset in "$published_assets"/*; do gh release verify-asset "$tag" "$asset" -R liatrio/agent-governance-evidence; done
scripts/verify-release.sh --tag "$tag" --commit "$commit" --phase published --assets-dir "$published_assets"
gh workflow run verify-release.yml -R liatrio/agent-governance-evidence \
  --ref "$tag" \
  -f tag="$tag" -f commit="$commit" -f phase=published
```

Again check the published verification run's workflow revision, run commit,
and both native jobs against `$commit`; the published path skips the retriever
job and re-downloads from the public release APIs, and dispatching the
protected tag does not replace that evidence check.

A failure never authorizes replacement, bypass, or source cutover; publish a
new prerelease instead.
DW-14 remains separately tracked: invalid demo targets may retain partial
output.
