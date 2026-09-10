# Security policy

Please report a security issue privately to Ian Hundere through
https://github.com/liatrio/agent-governance-evidence/security/advisories/new.
Before the first publication, the coordinator must enable and read back that
GitHub private-reporting control; this URL is the intended route, not a claim
that the control is enabled today. Do not include secrets, credentials, or
exploit details in public issues.

This is experimental evidence tooling, not a production security service. The
demo uses a private ephemeral CA; generated VSA JSON is unsigned; and the
pinned AGT wheel and transitive wheels have the provenance limitations
documented in the README.
