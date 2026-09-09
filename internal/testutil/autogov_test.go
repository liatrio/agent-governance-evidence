package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAutoGovBinaryRequiresAnAbsoluteExecutableRegularFile(t *testing.T) {
	t.Setenv(autoGovBinaryEnv, "")
	if _, err := AutoGovBinary(); err == nil || !strings.Contains(err.Error(), "setup-autogov.sh") {
		t.Fatalf("unset %s error = %v", autoGovBinaryEnv, err)
	}

	t.Setenv(autoGovBinaryEnv, "relative/autogov")
	if _, err := AutoGovBinary(); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("relative %s error = %v", autoGovBinaryEnv, err)
	}

	missing := filepath.Join(t.TempDir(), "missing")
	t.Setenv(autoGovBinaryEnv, missing)
	if _, err := AutoGovBinary(); err == nil || !strings.Contains(err.Error(), "existing executable") {
		t.Fatalf("missing %s error = %v", autoGovBinaryEnv, err)
	}

	directory := t.TempDir()
	t.Setenv(autoGovBinaryEnv, directory)
	if _, err := AutoGovBinary(); err == nil || !strings.Contains(err.Error(), "regular executable") {
		t.Fatalf("directory %s error = %v", autoGovBinaryEnv, err)
	}

	nonExecutable := filepath.Join(t.TempDir(), "autogov")
	if err := os.WriteFile(nonExecutable, []byte("not run\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(autoGovBinaryEnv, nonExecutable)
	if _, err := AutoGovBinary(); err == nil || !strings.Contains(err.Error(), "executable by this process") {
		t.Fatalf("non-executable %s error = %v", autoGovBinaryEnv, err)
	}

	executable := filepath.Join(t.TempDir(), "autogov")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 99\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(autoGovBinaryEnv, executable)
	if got, err := AutoGovBinary(); err != nil || got != executable {
		t.Fatalf("executable %s = %q, %v", autoGovBinaryEnv, got, err)
	}
}

func TestAutoGovBinaryUsesCurrentProcessExecuteAccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "permission-mismatch")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The file owner has no execute bit, while another permission class does.
	// LookPath uses the platform's effective-access check where available; its
	// result is therefore the portable expectation even when tests run as root.
	if err := os.Chmod(path, 0o010); err != nil {
		t.Fatal(err)
	}
	_, wantErr := exec.LookPath(path)
	got, gotErr := ValidateAutoGovBinary(path)
	if wantErr != nil {
		if gotErr == nil || got != "" {
			t.Fatalf("ValidateAutoGovBinary(%q) = %q, %v; want effective-access rejection", path, got, gotErr)
		}
		return
	}
	if gotErr != nil || got != path {
		t.Fatalf("ValidateAutoGovBinary(%q) = %q, %v; want effective-access acceptance", path, got, gotErr)
	}
}
