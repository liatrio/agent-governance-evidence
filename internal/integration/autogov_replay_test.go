package integration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/liatrio/agent-governance-evidence/internal/demokit"
)

// issue #16 (github.com/liatrio/agent-governance-evidence/issues/16): the gate
// has no freshness or anti-replay binding. case_timestamps_valid only bounds
// the case interval relative to itself (<=300s, internally ordered); it never
// compares either signed statement's absolute time to now, to each other, or
// to the case interval's absolute epoch. the test-result statement carries no
// timestamp at all (see demokit.BuildTestResultStatement / pred.TestResult).
//
// this is a KNOWN-GAP PIN, not a passing security control. it documents the
// gate's actual current behaviour as an executable claim: a stale, validly
// signed test-result statement is admitted when paired with a freshly signed
// deployment statement reusing the same case id, correlation id, and
// reference digests. TestAgentGovernanceAdversarialEvidenceFailsClosed nearby
// is the model for negative fixtures in this file, but a fixture asserting
// FAILED here would itself fail (and break CI) because the gate does not
// reject this today. When a freshness fix lands (timestamp comparison, a
// pipeline-run nonce, or a receipt chain), THIS TEST MUST START FAILING and
// its assertions must flip to expect rejection. Do not "fix" this test by
// weakening it; fix the gate and then update the assertions.
func TestAgentGovernanceStaleTestResultReplayIsAdmitted_KnownGap(t *testing.T) {
	signer, err := demokit.NewSigner(agDemoIdentity, agDemoIssuer)
	if err != nil {
		t.Fatalf("failed to create demo signer: %v", err)
	}
	dir := t.TempDir()
	trustedRoot := filepath.Join(dir, "trusted-root.json")
	rootJSON, err := signer.TrustedRootJSON()
	if err != nil {
		t.Fatalf("failed to export trusted root: %v", err)
	}
	if err := os.WriteFile(trustedRoot, rootJSON, 0600); err != nil {
		t.Fatal(err)
	}

	built, err := demokit.BuildCase(agEvidencePath(t, "non-agt", "allowed-action"))
	if err != nil {
		t.Fatalf("failed to build case: %v", err)
	}

	// "run 1": the test-result statement is signed once and never re-signed.
	// everything below reuses these exact bytes as the stale artifact from an
	// earlier, otherwise-unrelated pipeline execution.
	staleTestResult, err := signer.SignStatement(built.TestResultStatement)
	if err != nil {
		t.Fatalf("failed to sign stale test-result statement: %v", err)
	}

	t.Run("stale test-result paired with a fresh deployment whose case interval is pinned to the unix epoch", func(t *testing.T) {
		// "run 2": a freshly built and freshly signed deployment statement
		// (new leaf cert, new RFC3161 timestamp — see demokit.Signer.SignStatement)
		// that reuses the same case id, correlation id, agent digest, and
		// testResult.statementDigest linkage as run 1, but pins the case
		// interval to 1970-01-01. case_timestamps_valid only checks the
		// interval's internal ordering and <=300s width, so this passes
		// go-schema validation and the gate identically to a same-day case.
		freshDeployment, _ := signModifiedDeployment(t, signer, built, func(body map[string]interface{}) {
			c := caseObject(t, body)
			c["startedAt"] = "1970-01-01T00:00:00Z"
			c["completedAt"] = "1970-01-01T00:00:01Z"
			c["decision"].(map[string]interface{})["observedAt"] = "1970-01-01T00:00:00Z"
			c["outcome"].(map[string]interface{})["observedAt"] = "1970-01-01T00:00:01Z"
		})

		attestations := filepath.Join(dir, "replay-epoch.jsonl")
		writeBundleLines(t, attestations, freshDeployment, staleTestResult)
		vsaOut := filepath.Join(dir, "replay-epoch-vsa.json")

		runErr := runAgentGovernanceOffline(t, attestations, trustedRoot, "sha256:"+built.AgentDigestHex, vsaOut)
		v := readVSA(t, vsaOut)

		// pinning today's gap: this must be admitted. if this assertion
		// starts failing, a freshness fix landed — update this test to
		// assert rejection instead of loosening or deleting it.
		if runErr != nil {
			t.Fatalf("known-gap regression: the epoch-pinned replay is no longer admitted (did a freshness fix land?): %v", runErr)
		}
		if v.Predicate.VerificationResult != "PASSED" {
			t.Fatalf("known-gap regression: VSA = %s, want PASSED (did a freshness fix land?)", v.Predicate.VerificationResult)
		}
	})

	t.Run("same stale test-result reused unmodified by a second, differently-shaped deployment statement", func(t *testing.T) {
		// a distinct deployment statement (different runtimePolicy.count, so
		// byte-different and independently signed) still links cleanly to the
		// SAME already-consumed test-result from run 1. nothing marks a
		// test-result statement as spent after one admission: the binding is
		// one-directional (the deployment commits to the test-result's exact
		// payload digest; the test-result commits only to fields the
		// deployment signer chose, never to the deployment's own digest), so
		// one signed test-result can back arbitrarily many deployments.
		secondDeployment, _ := signModifiedDeployment(t, signer, built, func(body map[string]interface{}) {
			body["runtimePolicy"].(map[string]interface{})["count"] = float64(2)
		})

		attestations := filepath.Join(dir, "replay-reuse.jsonl")
		writeBundleLines(t, attestations, secondDeployment, staleTestResult)
		vsaOut := filepath.Join(dir, "replay-reuse-vsa.json")

		runErr := runAgentGovernanceOffline(t, attestations, trustedRoot, "sha256:"+built.AgentDigestHex, vsaOut)
		v := readVSA(t, vsaOut)

		if runErr != nil {
			t.Fatalf("known-gap regression: reuse of the same test-result by a second deployment is no longer admitted: %v", runErr)
		}
		if v.Predicate.VerificationResult != "PASSED" {
			t.Fatalf("known-gap regression: VSA = %s, want PASSED", v.Predicate.VerificationResult)
		}
	})
}

// caseObject reaches into a decoded predicate body for its sole conformance
// case, failing loudly if the fixture shape changes underneath this test.
func caseObject(t *testing.T, body map[string]interface{}) map[string]interface{} {
	t.Helper()
	conformance, ok := body["conformance"].(map[string]interface{})
	if !ok {
		t.Fatal("predicate body carries no conformance object")
	}
	cases, ok := conformance["cases"].([]interface{})
	if !ok || len(cases) != 1 {
		t.Fatalf("predicate body conformance.cases = %v, want exactly one case", cases)
	}
	c, ok := cases[0].(map[string]interface{})
	if !ok {
		t.Fatal("predicate body conformance.cases[0] is not an object")
	}
	return c
}
