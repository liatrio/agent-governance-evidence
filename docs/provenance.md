# Provenance

The distribution repository is liatrio/agent-governance-evidence. It was
extracted from liatrio/autogov; the exact nine-commit mapping and signature
limits are retained in [MOVE_MAP.md](../MOVE_MAP.md). Historical identifiers
remain identifiers: the v0.1 predicate is
https://autogov.dev/attestation/agent-governance-deployment/v0.1, while the
demo admission-policy URI records the historical AutoGov source location.
Neither is a fetchable hosted schema.

The standalone baseline is signed commit
1f064c06b5c6876deb6b2f99c156fd9409c929f9, tree
c56fdaf845d95ef7975c1871afaf69792c2015cc. It is compatible through the
external AutoGov v1.4.0 verifier, not a Go dependency or SDK. A published
release has two distinct proofs: its Sigstore build attestation binds the
three archives and SHA256SUMS to the release workflow/source, while GitHub's
immutable-release attestation binds all five uploaded assets after publication.
