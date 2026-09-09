package testutil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	setupAutoGovModule  = "github.com/liatrio/autogov"
	setupAutoGovVersion = "v1.4.0"
	setupAutoGovSum     = "h1:aj+yhS852iL8dKgTaoKh8PYVxKS2zznkfp6SxO6I6Ac="
)

type autoGovCompatibility struct {
	Module    string `json:"module"`
	Version   string `json:"version"`
	ModuleSum string `json:"moduleSum"`
	Binary    string `json:"binary"`
}

func TestSetupAutoGovMatchesCompatibilityManifest(t *testing.T) {
	compatibility := loadAutoGovCompatibility(t)
	root := setupSandbox(t, successfulFakeGo(t, false))
	output, err := runSetupAutoGov(t, root)
	if err != nil {
		t.Fatalf("setup failed: %s", output)
	}
	want := filepath.Join(root, filepath.FromSlash(compatibility.Binary)) + "\n"
	if string(output) != want {
		t.Fatalf("setup output = %q, want %q", output, want)
	}
}

func TestSetupAutoGovPublishesExecutableAndCleansPrivateStaging(t *testing.T) {
	root := setupSandbox(t, successfulFakeGo(t, false))
	output, err := runSetupAutoGov(t, root)
	if err != nil {
		t.Fatalf("setup failed: %s", output)
	}
	destination := setupDestination(root)
	if string(output) != destination+"\n" {
		t.Fatalf("setup output = %q, want exact absolute path %q", output, destination+"\n")
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "#!/bin/sh\necho fake-autogov\n" {
		t.Fatalf("published verifier bytes = %q", got)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("published verifier mode = %o, want 755", info.Mode().Perm())
	}
	staging, err := filepath.Glob(filepath.Join(root, ".setup-autogov.*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(staging) != 0 {
		t.Fatalf("private staging remained: %v", staging)
	}
}

func TestSetupAutoGovUsesFreshIsolatedGoEnvironment(t *testing.T) {
	root := setupSandbox(t, successfulFakeGo(t, false))
	ambientCache := filepath.Join(root, "ambient-module-cache")
	ambientBuildCache := filepath.Join(root, "ambient-build-cache")
	if err := os.Mkdir(ambientCache, 0o700); err != nil {
		t.Fatal(err)
	}
	goenv := filepath.Join(root, "hostile-goenv")
	if err := os.WriteFile(goenv, []byte("GOFLAGS=-modfile=/hostile.mod\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output, err := runSetupAutoGovWithEnv(t, root,
		"GOMODCACHE="+ambientCache,
		"GOCACHE="+ambientBuildCache,
		"GOWORK="+filepath.Join(root, "hostile.work"),
		"GOFLAGS=-modfile="+filepath.Join(root, "hostile.mod")+" -overlay="+filepath.Join(root, "hostile.json"),
		"GOENV="+goenv,
		"GOTOOLCHAIN=local",
		"ASSERT_ISOLATED_GO=1",
		"AMBIENT_GOMODCACHE="+ambientCache,
		"AMBIENT_GOCACHE="+ambientBuildCache,
	)
	if err != nil {
		t.Fatalf("setup did not isolate hostile Go environment: %s", output)
	}
}

func TestSetupAutoGovFailsWithoutPublishingWhenDownloadFails(t *testing.T) {
	root := setupSandbox(t, "echo download-failed >&2\nexit 41")
	output, err := runSetupAutoGov(t, root)
	if err == nil {
		t.Fatal("setup unexpectedly succeeded after download failure")
	}
	if pathExists(setupDestination(root)) {
		t.Fatal("failed setup published a verifier")
	}
	if !strings.Contains(string(output), "download-failed") {
		t.Fatalf("download failure output = %s", output)
	}
}

func TestSetupAutoGovRejectsPinMismatchWithoutPublishing(t *testing.T) {
	root := setupSandbox(t, `
if [ "$1" = mod ]; then
  printf '%s\n' '{' '  "Version": "v1.4.0",' '  "Sum": "h1:not-the-pinned-sum",' '  "Dir": "/missing",' '  "GoMod": "/missing/go.mod"'
  printf '%s\n' '}'
  exit 0
fi
exit 99`)
	output, err := runSetupAutoGov(t, root)
	if err == nil {
		t.Fatal("setup accepted a mismatched module sum")
	}
	if pathExists(setupDestination(root)) {
		t.Fatal("pin mismatch published a verifier")
	}
	if !strings.Contains(string(output), "module identity mismatch") {
		t.Fatalf("pin mismatch output = %s", output)
	}
}

func TestSetupAutoGovNeverOverwritesAnExistingVerifier(t *testing.T) {
	root := setupSandbox(t, successfulFakeGo(t, false))
	destination := setupDestination(root)
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		t.Fatal(err)
	}
	const original = "installed verifier\n"
	if err := os.WriteFile(destination, []byte(original), 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := runSetupAutoGov(t, root)
	if err == nil {
		t.Fatal("setup overwrote an existing verifier")
	}
	got, readErr := os.ReadFile(destination)
	if readErr != nil || string(got) != original {
		t.Fatalf("existing verifier changed: %q, %v", got, readErr)
	}
	if !strings.Contains(string(output), "refusing to overwrite") {
		t.Fatalf("collision output = %s", output)
	}
}

func TestSetupAutoGovRejectsDirectoryAndSymlinkDestinations(t *testing.T) {
	for _, kind := range []string{"directory", "directory symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := setupSandbox(t, successfulFakeGo(t, false))
			destination := setupDestination(root)
			if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
				t.Fatal(err)
			}
			protected := destination
			if kind == "directory" {
				if err := os.Mkdir(destination, 0o700); err != nil {
					t.Fatal(err)
				}
			} else {
				protected = filepath.Join(root, "protected-directory")
				if err := os.Mkdir(protected, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(protected, destination); err != nil {
					t.Fatal(err)
				}
			}

			output, err := runSetupAutoGov(t, root)
			if err == nil {
				t.Fatal("setup accepted a non-file verifier destination")
			}
			if _, err := os.Lstat(destination); err != nil {
				t.Fatalf("destination was removed: %v", err)
			}
			if pathExists(filepath.Join(protected, "autogov")) {
				t.Fatal("setup wrote a verifier inside a destination directory")
			}
			if !strings.Contains(string(output), "refusing to overwrite") {
				t.Fatalf("non-file collision output = %s", output)
			}
		})
	}
}

func TestSetupAutoGovRejectsSymlinkedStorageParents(t *testing.T) {
	for _, parent := range []string{".autogov", "bin"} {
		t.Run(parent, func(t *testing.T) {
			root := setupSandbox(t, successfulFakeGo(t, false))
			protected := filepath.Join(t.TempDir(), "protected")
			if err := os.Mkdir(protected, 0o700); err != nil {
				t.Fatal(err)
			}
			sentinel := filepath.Join(protected, "sentinel")
			const contents = "do not write outside verifier storage\n"
			if err := os.WriteFile(sentinel, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}

			storage := filepath.Join(root, ".autogov")
			if parent == ".autogov" {
				if err := os.Symlink(protected, storage); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Mkdir(storage, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(protected, filepath.Join(storage, "bin")); err != nil {
					t.Fatal(err)
				}
			}

			output, err := runSetupAutoGov(t, root)
			if err == nil {
				t.Fatal("setup accepted a symlinked storage parent")
			}
			got, readErr := os.ReadFile(sentinel)
			if readErr != nil || string(got) != contents {
				t.Fatalf("protected sentinel changed: %q, %v", got, readErr)
			}
			if pathExists(filepath.Join(protected, "autogov-v1.4.0")) {
				t.Fatal("setup wrote through a storage symlink")
			}
			if !strings.Contains(string(output), "symlinked AutoGov verifier storage parent") {
				t.Fatalf("symlinked-parent output = %s", output)
			}
		})
	}
}

func TestSetupAutoGovDoesNotOverwriteAConcurrentVerifier(t *testing.T) {
	root := setupSandbox(t, successfulFakeGo(t, true))
	marker := filepath.Join(root, "build-started")
	continueFile := filepath.Join(root, "continue-build")
	cmd := exec.Command(filepath.Join(root, "scripts", "setup-autogov.sh"))
	cmd.Env = append(os.Environ(),
		"PATH="+filepath.Join(root, "fakebin")+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FAKE_BUILD_MARKER="+marker,
		"FAKE_BUILD_CONTINUE="+continueFile,
		"EXPECTED_MODULE="+setupAutoGovModule,
		"EXPECTED_VERSION="+setupAutoGovVersion,
		"EXPECTED_SUM="+setupAutoGovSum,
	)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for !pathExists(marker) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !pathExists(marker) {
		_ = cmd.Process.Kill()
		t.Fatal("fake build did not begin")
	}
	destination := setupDestination(root)
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		t.Fatal(err)
	}
	const original = "concurrently installed verifier\n"
	if err := os.WriteFile(destination, []byte(original), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(continueFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("setup overwrote a concurrently installed verifier")
	}
	got, err := os.ReadFile(destination)
	if err != nil || string(got) != original {
		t.Fatalf("concurrent verifier changed: %q, %v", got, err)
	}
	if !strings.Contains(output.String(), "refusing to overwrite") {
		t.Fatalf("concurrent collision output = %s", output.String())
	}
}

func setupSandbox(t *testing.T, fakeGo string) string {
	t.Helper()
	root := t.TempDir()
	source, err := os.ReadFile(filepath.Join("..", "..", "scripts", "setup-autogov.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "scripts", "setup-autogov.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, source, 0o700); err != nil {
		t.Fatal(err)
	}
	moduleDir := filepath.Join(root, "module")
	if err := os.MkdirAll(moduleDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte("module "+setupAutoGovModule+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeDir := filepath.Join(root, "fakebin")
	if err := os.MkdirAll(fakeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	program := "#!/bin/sh\nset -eu\n" + strings.ReplaceAll(fakeGo, "${MODULE_DIR}", moduleDir) + "\n"
	if err := os.WriteFile(filepath.Join(fakeDir, "go"), []byte(program), 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}

func successfulFakeGo(t *testing.T, wait bool) string {
	t.Helper()
	waitForContinue := ""
	if wait {
		waitForContinue = `
: "${FAKE_BUILD_MARKER:?}"
: "${FAKE_BUILD_CONTINUE:?}"
: > "$FAKE_BUILD_MARKER"
while [ ! -e "$FAKE_BUILD_CONTINUE" ]; do sleep 0.01; done`
	}
	return fmt.Sprintf(`
if [ "$1" = mod ] && [ "$2" = download ]; then
  [ "$4" = "$EXPECTED_MODULE@$EXPECTED_VERSION" ] || exit 71
  if [ "${ASSERT_ISOLATED_GO:-}" = 1 ]; then
    [ "$GOWORK" = off ] || exit 72
    [ "$GOFLAGS" = -mod=readonly ] || exit 73
    [ "$GOTOOLCHAIN" = go1.26.6 ] || exit 74
    [ "$GOMODCACHE" != "$AMBIENT_GOMODCACHE" ] || exit 75
    case "$GOMODCACHE" in */.setup-autogov.*/modcache) ;; *) exit 76;; esac
    [ "$GOCACHE" != "$AMBIENT_GOCACHE" ] || exit 81
    case "$GOCACHE" in */.setup-autogov.*/buildcache) ;; *) exit 82;; esac
  fi
  printf '%%s\n' '{' '  "Version": "'$EXPECTED_VERSION'",' '  "Sum": "'$EXPECTED_SUM'",'
  printf '  "Dir": "%%s",\n' "${MODULE_DIR}"
  printf '%%s\n' '  "GoMod": "${MODULE_DIR}/go.mod"' '}'
  exit 0
fi
if [ "$1" = build ] && [ "$2" = -o ]; then%s
  if [ "${ASSERT_ISOLATED_GO:-}" = 1 ]; then
    [ "$GOWORK" = off ] || exit 77
    [ "$GOFLAGS" = -mod=readonly ] || exit 78
    [ "$GOTOOLCHAIN" = go1.26.6 ] || exit 79
    [ "$GOMODCACHE" != "$AMBIENT_GOMODCACHE" ] || exit 80
    [ "$GOCACHE" != "$AMBIENT_GOCACHE" ] || exit 83
    [ "$PWD" = "${MODULE_DIR}" ] || exit 84
  fi
  printf '#!/bin/sh\necho fake-autogov\n' > "$3"
  exit 0
fi
exit 99`, waitForContinue)
}

func runSetupAutoGov(t *testing.T, root string) ([]byte, error) {
	t.Helper()
	return runSetupAutoGovWithEnv(t, root)
}

func runSetupAutoGovWithEnv(t *testing.T, root string, extraEnv ...string) ([]byte, error) {
	t.Helper()
	cmd := exec.Command(filepath.Join(root, "scripts", "setup-autogov.sh"))
	compatibility := loadAutoGovCompatibility(t)
	cmd.Env = append(os.Environ(),
		"PATH="+filepath.Join(root, "fakebin")+string(os.PathListSeparator)+os.Getenv("PATH"),
		"EXPECTED_MODULE="+compatibility.Module,
		"EXPECTED_VERSION="+compatibility.Version,
		"EXPECTED_SUM="+compatibility.ModuleSum,
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	return cmd.CombinedOutput()
}

func setupDestination(root string) string {
	return filepath.Join(root, ".autogov", "bin", "autogov-v1.4.0")
}

func loadAutoGovCompatibility(t *testing.T) autoGovCompatibility {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "compatibility", "autogov.json"))
	if err != nil {
		t.Fatal(err)
	}
	var compatibility autoGovCompatibility
	if err := json.Unmarshal(data, &compatibility); err != nil {
		t.Fatal(err)
	}
	if compatibility.Module != setupAutoGovModule || compatibility.Version != setupAutoGovVersion || compatibility.ModuleSum != setupAutoGovSum || compatibility.Binary == "" {
		t.Fatal("unexpected AutoGov compatibility manifest")
	}
	return compatibility
}

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
