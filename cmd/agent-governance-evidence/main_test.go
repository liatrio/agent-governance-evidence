package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// minimal valid evidence document mirroring the schema contract example.
const agentGovernanceTestEvidence = `{
  "schemaVersion": "0.1",
  "agent": {
    "name": "agent-image",
    "uri": "urn:example:agent:write-marker",
    "artifactDigest": "sha256:0000000000000000000000000000000000000000000000000000000000000000"
  },
  "runtime": {
    "name": "example-runtime",
    "version": "1.2.3",
    "artifact": {
      "uri": "urn:example:runtime:1.2.3",
      "digest": "sha256:1111111111111111111111111111111111111111111111111111111111111111"
    }
  },
  "adapter": {
    "name": "non-agt-fixture",
    "artifact": {
      "uri": "urn:example:adapter:non-agt",
      "digest": "sha256:2222222222222222222222222222222222222222222222222222222222222222"
    },
    "contractVersion": "0.1",
    "runtimeDigest": "sha256:1111111111111111111111111111111111111111111111111111111111111111"
  },
  "runtimePolicy": {
    "engine": "opa",
    "artifact": {
      "uri": "urn:example:runtime-policy:fixture",
      "digest": "sha256:3333333333333333333333333333333333333333333333333333333333333333"
    },
    "count": 1,
    "loaded": true
  },
  "enforcement": {
    "mode": "enforce",
    "defaultBehavior": "deny",
    "requiredInterventionPoints": ["tool.pre"],
    "observedInterventionPoints": ["tool.pre"]
  },
  "identity": {
    "providerUri": "urn:example:identity-provider",
    "subjectKind": "workload",
    "subject": {
      "id": "redacted:fixture-workload",
      "digest": "sha256:4444444444444444444444444444444444444444444444444444444444444444"
    }
  },
  "audit": {
    "sinkKind": "file",
    "sink": {
      "id": "redacted:fixture-audit-sink",
      "digest": "sha256:5555555555555555555555555555555555555555555555555555555555555555"
    },
    "configurationDigest": "sha256:6666666666666666666666666666666666666666666666666666666666666666"
  },
  "conformance": {
    "fixture": {"id": "non-agt-allowed-001", "producer": "non-agt"},
    "controlledTool": {
      "name": "write-marker",
      "actionClass": "filesystem.write.marker",
      "artifact": {
        "uri": "urn:autogov:fixture:write-marker",
        "digest": "sha256:7777777777777777777777777777777777777777777777777777777777777777"
      }
    },
    "cases": [{
      "id": "allowed-action-001",
      "kind": "allowed-action",
      "correlationId": "sha256:8888888888888888888888888888888888888888888888888888888888888888",
      "startedAt": "2026-08-26T20:00:00Z",
      "completedAt": "2026-08-26T20:00:01Z",
      "decision": {
        "state": "observed",
        "verdict": "allow",
        "reference": {
          "id": "redacted:decision-001",
          "digest": "sha256:9999999999999999999999999999999999999999999999999999999999999999"
        },
        "observedAt": "2026-08-26T20:00:00Z"
      },
      "outcome": {
        "state": "verified",
        "result": "occurred",
        "reference": {
          "id": "redacted:outcome-001",
          "digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
        },
        "observedAt": "2026-08-26T20:00:01Z"
      },
      "testResult": {
        "predicateType": "https://in-toto.io/attestation/test-result/v0.1",
        "testId": "allowed-action-001",
        "statementDigest": "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
      }
    }]
  }
}`

func TestAgentGovernanceDeploymentCommand(t *testing.T) {
	dir := t.TempDir()
	evidencePath := filepath.Join(dir, "evidence.json")
	outputPath := filepath.Join(dir, "predicate.json")

	if err := os.WriteFile(evidencePath, []byte(agentGovernanceTestEvidence), 0600); err != nil {
		t.Fatal(err)
	}

	if err := run([]string{"--evidence-path", evidencePath, "--output", outputPath}); err != nil {
		t.Fatalf("command failed: %v", err)
	}

	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("expected output file: %v", err)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(content, &body); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	// the command emits only the predicate body
	if _, exists := body["predicateType"]; exists {
		t.Error("output must not contain envelope fields")
	}
	if body["schemaVersion"] != "0.1" {
		t.Errorf("schemaVersion = %v, want 0.1", body["schemaVersion"])
	}
}

func TestAgentGovernanceDeploymentCommandStdoutMatchesFileBytes(t *testing.T) {
	dir := t.TempDir()
	evidencePath := filepath.Join(dir, "evidence.json")
	outputPath := filepath.Join(dir, "predicate.json")
	if err := os.WriteFile(evidencePath, []byte(agentGovernanceTestEvidence), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"--evidence-path", evidencePath, "--output", outputPath}); err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}

	captured, err := os.CreateTemp(dir, "stdout-")
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = captured
	t.Cleanup(func() { os.Stdout = original })
	var stderr bytes.Buffer
	exitCode := execute([]string{"--evidence-path", evidencePath}, &stderr)
	os.Stdout = original
	if exitCode != 0 {
		t.Fatalf("stdout command exit = %d, want 0; stderr=%s", exitCode, stderr.String())
	}
	if _, err := captured.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(captured)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("stdout differs from file output:\nstdout: %q\nfile:   %q", got, want)
	}
}

func TestAgentGovernanceDeploymentCommandFailsClosed(t *testing.T) {
	dir := t.TempDir()
	evidencePath := filepath.Join(dir, "evidence.json")
	outputPath := filepath.Join(dir, "predicate.json")

	// duplicate case id via a second identical case
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(agentGovernanceTestEvidence), &m); err != nil {
		t.Fatal(err)
	}
	conformance := m["conformance"].(map[string]interface{})
	cases := conformance["cases"].([]interface{})
	conformance["cases"] = append(cases, cases[0])
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(evidencePath, raw, 0600); err != nil {
		t.Fatal(err)
	}

	if err := run([]string{"--evidence-path", evidencePath, "--output", outputPath}); err == nil {
		t.Fatal("expected the command to fail for duplicate case ids")
	}
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Errorf("expected no partial output file, stat err = %v", err)
	}

	// missing evidence file
	if err := run([]string{"--evidence-path", filepath.Join(dir, "missing.json")}); err == nil {
		t.Error("expected the command to fail for a missing evidence file")
	}
}

func TestAgentGovernanceDeploymentCLIRejectsInvalidEvidenceWithoutDisclosure(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "agent-governance-evidence")
	if output, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build companion CLI: %v\n%s", err, output)
	}

	const memberCanary = "secret-cli-member-7c927df09e6b4e8a"
	const numberCanary = "9385417260964213e9999"
	cases := []struct {
		name, input, canary string
	}{
		{
			name:   "unknown member",
			input:  strings.Replace(agentGovernanceTestEvidence, "{", "{\""+memberCanary+"\":true,", 1),
			canary: memberCanary,
		},
		{
			name:   "unrepresentable number",
			input:  strings.Replace(agentGovernanceTestEvidence, `"count": 1`, `"count": `+numberCanary, 1),
			canary: numberCanary,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			caseDir := t.TempDir()
			evidencePath := filepath.Join(caseDir, "evidence.json")
			outputPath := filepath.Join(caseDir, "predicate.json")
			if err := os.WriteFile(evidencePath, []byte(tc.input), 0o600); err != nil {
				t.Fatal(err)
			}

			var stdout, stderr bytes.Buffer
			cmd := exec.Command(binary, "--evidence-path", evidencePath, "--output", outputPath)
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			var exitErr *exec.ExitError
			if err := cmd.Run(); !errors.As(err, &exitErr) || exitErr.ExitCode() == 0 {
				t.Fatalf("invalid evidence must exit nonzero, got %v", err)
			}
			if strings.Contains(stderr.String(), tc.canary) {
				t.Errorf("CLI stderr disclosed secret JSON content: %s", stderr.String())
			}
			if !strings.Contains(stderr.String(), "invalid agent-governance evidence") {
				t.Errorf("CLI did not identify invalid evidence: %s", stderr.String())
			}
			if stderr.Len() > 512 {
				t.Errorf("CLI diagnostic exceeded 512 bytes: %d", stderr.Len())
			}
			if stdout.Len() != 0 {
				t.Errorf("invalid evidence wrote %d bytes to stdout", stdout.Len())
			}
			if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
				t.Errorf("invalid evidence created an output file: %v", err)
			}
		})
	}
}

func TestAgentGovernanceDeploymentCommandRequiresEvidence(t *testing.T) {
	if err := run(nil); err == nil {
		t.Fatal("expected missing --evidence-path to fail")
	}
	if err := run([]string{"unexpected"}); err == nil {
		t.Fatal("expected positional arguments to fail")
	}
}

func TestExecuteMapsHelpErrorsAndSuccessToExitCodes(t *testing.T) {
	var stderr bytes.Buffer
	if got := execute([]string{"--help"}, &stderr); got != 0 {
		t.Errorf("help exit = %d, want 0", got)
	}
	if got := execute(nil, &stderr); got != 1 {
		t.Errorf("missing evidence exit = %d, want 1", got)
	}

	dir := t.TempDir()
	evidencePath := filepath.Join(dir, "evidence.json")
	outputPath := filepath.Join(dir, "predicate.json")
	if err := os.WriteFile(evidencePath, []byte(agentGovernanceTestEvidence), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := execute([]string{"--evidence-path", evidencePath, "--output", outputPath}, &stderr); got != 0 {
		t.Fatalf("valid evidence exit = %d, want 0; stderr=%s", got, stderr.String())
	}
}
