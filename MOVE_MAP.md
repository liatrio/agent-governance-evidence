# Agent-governance extraction move map

This map records the history-preserving relocation from the autogov spike at
commit `c11a1b0fbe02de266ba11963abedd0d07f427be9` into the repository-local
companion. The relocation commit changes paths only; package and behavior
changes follow separately.

The deterministic pre-extraction checkpoint was committed as
`c4b06485139f1fc579eda277e605eece461f13c9`. The pure path relocation was then
committed as `db42d1644d4d9bfc8e2917e3b22919c81c5b7faf`, preserving rename history
before any boundary-isolation edits.

| Old path | Companion path |
| --- | --- |
| `examples/agent-governance/README.md` | `agent-governance/README.md` |
| `examples/agent-governance/checkpoint.sha256.json` | `agent-governance/checkpoint.sha256.json` |
| `examples/agent-governance/MOVE_MAP.md` | `agent-governance/MOVE_MAP.md` |
| `examples/agent-governance/adapters/**` | `agent-governance/adapters/**` |
| `examples/agent-governance/fixtures/**` | `agent-governance/fixtures/**` |
| `examples/agent-governance/policy/**` | `agent-governance/policy/**` |
| `examples/agent-governance/cmd/checkpoint/**` | `agent-governance/cmd/checkpoint/**` |
| `examples/agent-governance/cmd/demo/**` | `agent-governance/cmd/demo/**` |
| `examples/agent-governance/demokit/**` | `agent-governance/internal/demokit/**` |
| `pkg/predicate/agent_governance_deployment.go` | `agent-governance/internal/evidence/deployment.go` |
| `pkg/predicate/agent_governance_deployment_test.go` | `agent-governance/internal/evidence/deployment_test.go` |
| `pkg/predicate/schemas/agent-governance-deployment-schema.json` | `agent-governance/internal/evidence/schemas/agent-governance-deployment-schema.json` |
| `cmd/predicate/agent_governance_deployment.go` | `agent-governance/cmd/agent-governance-evidence/main.go` |
| `cmd/predicate/agent_governance_deployment_test.go` | `agent-governance/cmd/agent-governance-evidence/main_test.go` |
| `pkg/offline/agent_governance_integration_test.go` | `agent-governance/internal/integration/autogov_e2e_test.go` |

Ignored environments and caches under `.venv/`, `.wheels/`, and
`__pycache__/` are deliberately excluded from the move.

The post-move isolation commit
`7e572a88a3439ee8887f17905356dc1ed6dc501d` also copies four small
autogov-owned helpers so neither dependency graph crosses the artifact/CLI
boundary:

| autogov source | Companion-local copy |
| --- | --- |
| `pkg/predicate/predicate.go` (`writeOutput`) | `agent-governance/internal/evidence/output.go` |
| `pkg/predicate/config.go` (embedded-schema validation) | `agent-governance/internal/evidence/schema.go` |
| `pkg/predicate/testresult.go` (wire types/constants) | `agent-governance/internal/evidence/testresult.go` |
| `pkg/predicate/schemas/test-result-schema.json` | `agent-governance/internal/evidence/schemas/test-result-schema.json` |

That same isolation commit copied the signer in the other direction: the
domain-neutral autogov-private helper at `pkg/offline/test_signer_test.go`
originated from `agent-governance/internal/demokit/signer.go`. The copy keeps
autogov's production and test packages independent of companion Go imports.

The demo's VSA policy metadata migration is deliberate: `predicate.policy.uri`
changed from `https://github.com/liatrio/autogov/examples/agent-governance/policy`
to `https://github.com/liatrio/autogov/agent-governance/policy`. The policy
bytes and the content digest recorded by `checkpoint.sha256.json` are unchanged.

## Standalone extraction provenance

Standalone readiness was prepared from source revision
`66394bbf1c0f6ded20a167ce6b646b66d35fe20e`, which includes local
demo-reliability changes after the immutable autogov v1.4.0 source commit
`139fc0f3fe3f1ec7e7c9f0e9768f2b17c183e90e`. The extracted repository baseline
is `342a40e3b315ba77958c8382ffe68c48ef7f7e4e`. The companion retains autogov's
Apache-2.0 [`LICENSE`](LICENSE), the move history above, and the immutable
checkpoint. Its Go module is now `github.com/liatrio/agent-governance-evidence`;
the supplied autogov verifier remains an external executable rather than a Go
dependency or replacement.

## Final standalone mapping

The final standalone root maps the autogov `agent-governance/**` tree as
follows:

| autogov path | Standalone path |
| --- | --- |
| `agent-governance/README.md` | `README.md` |
| `agent-governance/MOVE_MAP.md` | `MOVE_MAP.md` |
| `agent-governance/checkpoint.sha256.json` | `checkpoint.sha256.json` |
| `agent-governance/adapters/**` | `adapters/**` |
| `agent-governance/fixtures/**` | `fixtures/**` |
| `agent-governance/policy/**` | `policy/**` |
| `agent-governance/cmd/**` | `cmd/**` |
| `agent-governance/internal/**` | `internal/**` |

The extracted repository has distinct commit identities and signatures from
the autogov source history. Original source signatures are not preserved in
the extracted history; the historical mapping above remains the provenance
record.

## Canonical extraction-to-publication mapping

Publication retains the following exact original-to-extracted commit mapping.
The nine original commits were signed; the path-relocated extracted commits are
unsigned, so source signatures remain verifiable only in liatrio/autogov.

| Original commit | Extracted commit |
|---|---|
| 6630b21df5ea247eca2ad6310f5294731502c765 | b0bbf547aa3c30215132d9efb76a0edcd1bda094 |
| c11a1b0fbe02de266ba11963abedd0d07f427be9 | fae418d917c2444ad75f75afa6b49dcf588d5cc0 |
| c4b06485139f1fc579eda277e605eece461f13c9 | 7ed5ed081d7b2557bde50512d55f73620ee9d523 |
| db42d1644d4d9bfc8e2917e3b22919c81c5b7faf | 720fc1ca4c53f0c68e0ef7ae73f74f5b4d98a3b4 |
| 7e572a88a3439ee8887f17905356dc1ed6dc501d | e06bdb40c4d595572ff6dc64cf39d43be705a01e |
| ca668161bdaec408f194b6ada527e2b5f7f4b444 | b1489de5698b93d10845e7d136ad930ed2868ba5 |
| 3d45019bc1f95a4440d300f6df20737535905371 | bfd283671e2e46932f1dc87466abb642afb801e3 |
| fc7128769e9da2921e09d005d756983bb09759dd | aa6515f8872b03d40327fc0295e4c3402caaabce |
| 66394bbf1c0f6ded20a167ce6b646b66d35fe20e | 342a40e3b315ba77958c8382ffe68c48ef7f7e4e |

The initial extracted 71-file tree was
29ac72f1ecba63c0c1f799c59ad602755beb2ae8; signed standalone baseline
1f064c06b5c6876deb6b2f99c156fd9409c929f9 has tree
c56fdaf845d95ef7975c1871afaf69792c2015cc and adds the unchanged Apache-2.0
license. autogov v1.4.0 remains the compatibility source; its module sum is
h1:aj+yhS852iL8dKgTaoKh8PYVxKS2zznkfp6SxO6I6Ac=.

## Forward source cutover

The forward source removal landed in
[liatrio/autogov#386](https://github.com/liatrio/autogov/pull/386) at commit
`c48d0e02b112bdad0a086e46d452cfdad9cf896a`. It removes the tracked
`agent-governance/**` subtree from current `autogov` `main` while preserving
the generic verification, security, and VSA behavior that shipped in PR #379.

Historical autogov tags, releases, and preserved source history still contain
the old bundled tree. New work on this experiment belongs here in
`liatrio/agent-governance-evidence`, and any future correction to release-facing
claims ships by normal forward PR or new prerelease rather than mutating old
release history.
