# Releasing

Only Ian Hundere coordinates a release. Before tagging, record green hosted
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
the four build subjects, and creates a draft prerelease. Download exactly five
assets into a fresh directory and run:

```bash
assets=$(mktemp -d "${TMPDIR:-/tmp}/agent-governance-draft.XXXXXX")
gh release download "$tag" -R liatrio/agent-governance-evidence \
  --dir "$assets" --pattern '*'
scripts/verify-release.sh --tag "$tag" --commit "$commit" --phase draft --assets-dir "$assets"
gh workflow run verify-release.yml -R liatrio/agent-governance-evidence \
  --ref "$tag" \
  -f tag="$tag" -f commit="$commit" -f phase=draft
```

Check that the verification workflow revision/run commit and both native jobs
match `$commit`. Re-read immutable-release enablement, both tag rulesets, and
the tag's peeled commit immediately before the coordinator explicitly
publishes. No draft, source asset, or tag is overwritten: a collision or
defect requires a new prerelease.

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
and both native jobs against `$commit`; dispatching the protected tag does not
replace that evidence check.

A failure never authorizes replacement, bypass, or source cutover; publish a
new prerelease instead.
DW-14 remains separately tracked: invalid demo targets may retain partial
output.
