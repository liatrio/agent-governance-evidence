# Agent-governance extraction move map

This map records the history-preserving relocation from the AutoGov spike at
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
`7e572a88a3439ee8887f17905356dc1ed6dc501d` also duplicates four small
AutoGov-owned helpers so neither dependency graph crosses the artifact/CLI
boundary:

| AutoGov source | Companion-local copy |
| --- | --- |
| `pkg/predicate/predicate.go` (`writeOutput`) | `agent-governance/internal/evidence/output.go` |
| `pkg/predicate/config.go` (embedded-schema validation) | `agent-governance/internal/evidence/schema.go` |
| `pkg/predicate/testresult.go` (wire types/constants) | `agent-governance/internal/evidence/testresult.go` |
| `pkg/predicate/schemas/test-result-schema.json` | `agent-governance/internal/evidence/schemas/test-result-schema.json` |

That same isolation commit copied the signer in the other direction: the
domain-neutral AutoGov-private helper at `pkg/offline/test_signer_test.go`
originated from `agent-governance/internal/demokit/signer.go`. The copy keeps
AutoGov's production and test packages independent of companion Go imports.

The demo's VSA policy metadata migration is deliberate: `predicate.policy.uri`
changed from `https://github.com/liatrio/autogov/examples/agent-governance/policy`
to `https://github.com/liatrio/autogov/agent-governance/policy`. The policy
bytes and the content digest recorded by `checkpoint.sha256.json` are unchanged.

## Standalone extraction provenance

Standalone readiness was prepared from source revision
`66394bbf1c0f6ded20a167ce6b646b66d35fe20e`, which includes local
demo-reliability changes after the immutable AutoGov v1.4.0 source commit
`139fc0f3fe3f1ec7e7c9f0e9768f2b17c183e90e`. The extracted repository baseline
is `342a40e3b315ba77958c8382ffe68c48ef7f7e4e`. The companion retains AutoGov's
Apache-2.0 [`LICENSE`](LICENSE), the move history above, and the immutable
checkpoint. Its Go module is now `github.com/liatrio/agent-governance-evidence`;
the supplied AutoGov verifier remains an external executable rather than a Go
dependency or replacement.

## Final standalone mapping

The final standalone root maps the AutoGov `agent-governance/**` tree as
follows:

| AutoGov path | Standalone path |
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
the AutoGov source history. Original source signatures are not preserved in
the extracted history; the historical mapping above remains the provenance
record.
