package testutil

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const autoGovBinaryEnv = "AUTOGOV_BINARY"

// AutoGovBinary returns the explicitly configured verifier for black-box tests.
func AutoGovBinary() (string, error) {
	return ValidateAutoGovBinary(os.Getenv(autoGovBinaryEnv))
}

// ValidateAutoGovBinary accepts only an absolute executable regular file.
func ValidateAutoGovBinary(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("%s is required; run ./scripts/setup-autogov.sh and export its printed path", autoGovBinaryEnv)
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("%s must be an absolute path, got %q", autoGovBinaryEnv, path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("%s must name an existing executable: %w", autoGovBinaryEnv, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s must name a regular executable file, got %s", autoGovBinaryEnv, info.Mode().Type())
	}
	if _, err := exec.LookPath(path); err != nil {
		return "", fmt.Errorf("%s must name a file executable by this process: %w", autoGovBinaryEnv, err)
	}
	return path, nil
}
