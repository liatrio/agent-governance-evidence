# Policy configuration examples

Operator-supplied overrides for the local admission gate in
[`policy/agent_governance.rego`](../../policy/agent_governance.rego). Values are
supplied at runtime via `--policy-data-path`; no fork or policy edit is needed.

These files deliberately live **outside** `policy/`. `autogov` loads only
`.rego` files from `--policy-bundle-path` as policy, and treats every `.json`
found under the schemas path — which defaults to the bundle path — as a JSON
schema. A data document placed in `policy/` would therefore be loaded as a
schema and never reach `data`, besides changing the checkpointed policy-bundle
digest.

## Usage

```bash
autogov offline \
  --attestations attestations.jsonl \
  --policy-bundle-path policy/ \
  --policy-data-path config/examples/default-allowlist.json \
  --generate-vsa --vsa-output vsa.json
```

## `approved_runtime_policy_digests`

The set of runtime-policy artifact digests the gate will admit. A deployment
statement whose `runtimePolicy.artifact.digest` is outside this set is not
admissible, because `deployment_enforcing` requires membership.

Absence of the key — or of the whole data document — applies the gate's
built-in default, which is deliberately non-empty. An unconfigured gate is
never an unconstrained one. Supplying an empty array admits nothing rather than
everything; membership in an empty set is unsatisfiable.

| File | Contents | Effect |
| --- | --- | --- |
| `default-allowlist.json` | both adapter runtime policies | identical to supplying no data document at all |
| `agt-only.json` | the AGT runtime policy only | admits the AGT producer, denies the non-AGT producer |
| `deny-all.json` | empty array | admits nothing |

The two default digests are the SHA-256 of the checked-in adapter runtime
policies, both of which are frozen inputs in `checkpoint.sha256.json`:

| Digest | Artifact |
| --- | --- |
| `sha256:5444ca77…dec6c` | `adapters/agt/runtime_policy.yaml` |
| `sha256:874d825f…1bf9` | `adapters/non-agt/runtime_policy.json` |

## Limits

`--policy-data-path` takes a local path with no digest pin of its own —
`--policy-bundle-digest` covers only `ghrel://` bundle paths and does not apply
here. Whoever can write the data document can widen the allowlist. This control
moves the runtime policy from "any shape-valid digest" to "a digest the
operator named"; it does not make the operator's data document tamper-evident.
