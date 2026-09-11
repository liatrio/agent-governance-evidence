# Provenance

The distribution repository is liatrio/agent-governance-evidence. It was
extracted from liatrio/autogov; the exact nine-commit mapping and signature
limits are retained in [MOVE_MAP.md](../MOVE_MAP.md). Historical identifiers
remain identifiers: the v0.1 predicate is
https://autogov.dev/attestation/agent-governance-deployment/v0.1, while the
demo admission-policy URI records the historical AutoGov source location.
Neither is a fetchable hosted schema.

The immutable standalone release evidence for that v0.1 contract is
[`v0.1.0-alpha.3`](https://github.com/liatrio/agent-governance-evidence/releases/tag/v0.1.0-alpha.3)
at commit `5801e9c5305e42c08543fb7b517a94bef9603ee3`. If a release-facing
claim ever needs correction, the fix ships in a new prerelease; it does not
rewrite or mutate published tags, assets, or release history.

The standalone baseline is signed commit
1f064c06b5c6876deb6b2f99c156fd9409c929f9, tree
c56fdaf845d95ef7975c1871afaf69792c2015cc. It is compatible through the
external AutoGov v1.4.0 verifier, not a Go dependency or SDK. A published
release has two distinct proofs: its Sigstore build attestation binds the
three archives and SHA256SUMS to the release workflow/source, while GitHub's
immutable-release attestation binds all five uploaded assets after publication.
That release evidence remains valid even after AutoGov removes the browsable
`agent-governance/` path from current `main`: the demo admission-policy URI is
historical identifier text, not a promise that the old source path remains
live forever.
