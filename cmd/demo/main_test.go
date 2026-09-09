package main

import (
	"encoding/json"
	"errors"
	"flag"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/liatrio/agent-governance-evidence/internal/testutil"
)

func TestPrepareWorkdirCreatesCallerSuppliedDirectoryWithoutRemovingIt(t *testing.T) {
	for _, keep := range []bool{false, true} {
		for _, suffix := range []string{"", string(os.PathSeparator)} {
			requested := filepath.Join(t.TempDir(), "nested", "demo-output") + suffix
			dir, cleanup, err := prepareWorkdir(requested, keep)
			if err != nil {
				t.Fatal(err)
			}
			if want := filepath.Clean(requested); dir != want {
				t.Errorf("workdir = %q, want %q", dir, want)
			}
			if info, err := os.Stat(requested); err != nil || !info.IsDir() {
				t.Fatalf("caller-supplied workdir was not created: info=%v err=%v", info, err)
			}
			cleanup()
			if _, err := os.Stat(requested); err != nil {
				t.Errorf("cleanup removed caller-supplied workdir: %v", err)
			}
		}
	}
}

func TestPrepareWorkdirReturnsUsableNormalizedPath(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"absolute", "relative"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			if kind == "relative" {
				root, err = filepath.Rel(cwd, root)
				if err != nil {
					t.Fatal(err)
				}
			}
			requested := root + string(os.PathSeparator) + "missing" + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "run"
			dir, cleanup, err := prepareWorkdir(requested, false)
			if err != nil {
				t.Fatal(err)
			}
			cleanup()
			if want := filepath.Clean(requested); dir != want {
				t.Errorf("workdir = %q, want normalized path %q", dir, want)
			}
			if info, err := os.Stat(dir); err != nil || !info.IsDir() {
				t.Errorf("returned workdir cannot be opened after cleanup: info=%v err=%v", info, err)
			}
		})
	}
}

func TestRunRejectsExistingWorkdirBeforeWritingOrExecuting(t *testing.T) {
	companion, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"empty directory", "prior evidence", "file", "directory symlink", "file symlink", "dangling symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			requested := filepath.Join(root, "run")
			switch kind {
			case "empty directory", "prior evidence":
				if err := os.Mkdir(requested, 0o750); err != nil {
					t.Fatal(err)
				}
				if kind == "prior evidence" {
					for name, body := range map[string]string{
						"trusted-root.json":                    "prior trusted root\n",
						"non-agt-allowed-action-vsa.json":      `{"predicate":{"verificationResult":"FAILED"}}`,
						"non-agt-allowed-action-evidence.json": "prior evidence\n",
						"sentinel":                             "caller-owned bytes\x00\n",
					} {
						if err := os.WriteFile(filepath.Join(requested, name), []byte(body), 0o600); err != nil {
							t.Fatal(err)
						}
					}
				}
			case "file":
				if err := os.WriteFile(requested, []byte("caller-owned file\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			default:
				target := filepath.Join(root, "target")
				switch kind {
				case "directory symlink":
					if err := os.Mkdir(target, 0o750); err != nil {
						t.Fatal(err)
					}
				case "file symlink":
					if err := os.WriteFile(target, []byte("caller-owned target\n"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Symlink(target, requested); err != nil {
					t.Fatal(err)
				}
			}
			autogov := filepath.Join(root, "autogov")
			evidence := filepath.Join(root, "agent-governance-evidence")
			for _, binary := range []string{autogov, evidence} {
				if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf 'executed\\n' >> \"$0.called\"\nexit 1\n"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			before := snapshotTree(t, root)
			for _, keep := range []bool{false, true} {
				if err := run(autogov, evidence, companion, requested, keep); !errors.Is(err, os.ErrExist) {
					t.Errorf("collision error = %v, want an already-existing path error", err)
				}
				if after := snapshotTree(t, root); !reflect.DeepEqual(after, before) {
					t.Errorf("collision changed caller content or executed a binary (tree entries before=%d after=%d)", len(before), len(after))
				}
			}
		})
	}
}

type treeEntry struct {
	mode    fs.FileMode
	content string
}

func snapshotTree(t *testing.T, root string) map[string]treeEntry {
	t.Helper()
	snapshot := make(map[string]treeEntry)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			snapshot[path] = treeEntry{mode: info.Mode()}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			snapshot[path] = treeEntry{mode: info.Mode(), content: target}
			return err
		}
		body, err := os.ReadFile(path)
		snapshot[path] = treeEntry{mode: info.Mode(), content: string(body)}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestPrepareWorkdirRemovesOnlyAutomaticTemporaryDirectory(t *testing.T) {
	dir, cleanup, err := prepareWorkdir("", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("automatic workdir was not created: %v", err)
	}
	cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("automatic workdir remains after cleanup: %v", err)
	}
}

func TestPrepareWorkdirKeepsAutomaticTemporaryDirectoryWhenRequested(t *testing.T) {
	dir, cleanup, err := prepareWorkdir("", true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	cleanup()
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("automatic workdir was not retained: info=%v err=%v", info, err)
	}
}

// the demo compares the production predicate command with demokit's
// deterministic library output before it signs every case. run that exact
// boundary in the ordinary Go suite so the check is not manual-only.
func TestDemoExercisesCLIAndLibraryBoundary(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary, err := testutil.AutoGovBinary()
	if err != nil {
		t.Fatalf("require external AutoGov verifier: %v", err)
	}
	evidenceBinary := filepath.Join(t.TempDir(), "agent-governance-evidence")
	buildEvidence := exec.Command("go", "build", "-o", evidenceBinary, "./cmd/agent-governance-evidence")
	buildEvidence.Dir = root
	if output, err := buildEvidence.CombinedOutput(); err != nil {
		t.Fatalf("build companion evidence binary: %v\n%s", err, output)
	}
	demoBinary := filepath.Join(t.TempDir(), "agent-governance-demo")
	buildDemo := exec.Command("go", "build", "-o", demoBinary, "./cmd/demo")
	buildDemo.Dir = root
	if output, err := buildDemo.CombinedOutput(); err != nil {
		t.Fatalf("build companion demo binary: %v\n%s", err, output)
	}
	workdir := filepath.Join(t.TempDir(), "nested", "demo-output")
	demo := exec.Command(demoBinary,
		"--autogov", binary,
		"--agent-governance-evidence", evidenceBinary,
		"--companion", root,
		"--workdir", workdir,
	)
	output, err := demo.CombinedOutput()
	if err != nil {
		t.Fatalf("run demo: %v\n%s", err, output)
	}
	_, table, found := strings.Cut(string(output), "---------  -----------------  --------  --------  ----  --\n")
	if !found {
		t.Fatalf("demo output has no observation table:\n%s", output)
	}
	table, _, found = strings.Cut(table, "\n\n")
	if !found {
		t.Fatalf("demo output has no complete observation table:\n%s", output)
	}
	var observations []string
	for _, line := range strings.Split(table, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 6 {
			t.Fatalf("malformed demo observation: %q", line)
		}
		observations = append(observations, strings.Join([]string{fields[0], fields[1], fields[3], fields[4]}, " "))
	}
	// pin observed VSA results and exact exits independently of production cases.
	want := []string{
		"non-agt allowed-action PASSED 0",
		"non-agt denied-action PASSED 0",
		"non-agt adapter-bypass FAILED 1",
		"non-agt no-policy-loaded FAILED 1",
		"non-agt unknown-outcome* FAILED 1",
		"agt allowed-action PASSED 0",
		"agt denied-action PASSED 0",
		"agt adapter-bypass FAILED 1",
		"agt no-policy-loaded FAILED 1",
		"agt unknown-outcome* FAILED 1",
	}
	if !reflect.DeepEqual(observations, want) {
		t.Fatalf("demo observations:\n%s\nwant:\n%s", strings.Join(observations, "\n"), strings.Join(want, "\n"))
	}
	artifacts := map[string]string{"trusted-root.json": ""}
	for _, row := range want {
		fields := strings.Fields(row)
		prefix := fields[0] + "-" + strings.TrimSuffix(fields[1], "*")
		for _, suffix := range []string{"-evidence.json", "-predicate.json", ".jsonl", "-vsa.json"} {
			artifacts[prefix+suffix] = ""
		}
		artifacts[prefix+"-vsa.json"] = fields[2]
	}
	for artifact, wantResult := range artifacts {
		path := filepath.Join(workdir, artifact)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			t.Errorf("demo artifact %s was not retained in the requested directory: info=%v err=%v", artifact, info, err)
			continue
		}
		if wantResult == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var vsa struct {
			Predicate struct {
				VerificationResult string `json:"verificationResult"`
			} `json:"predicate"`
		}
		if err := json.Unmarshal(data, &vsa); err != nil {
			t.Fatalf("decode saved VSA %s: %v", artifact, err)
		}
		if result := vsa.Predicate.VerificationResult; result != wantResult {
			t.Errorf("saved VSA %s result = %q, want %q", artifact, result, wantResult)
		}
	}
	if got := retainedFiles(t, workdir); !reflect.DeepEqual(got, sortedArtifactNames(artifacts)) {
		t.Fatalf("retained artifact set = %v, want exactly %v", got, sortedArtifactNames(artifacts))
	}
	beforeRetry := snapshotTree(t, workdir)
	retry := exec.Command(demoBinary, demo.Args[1:]...)
	retryOutput, err := retry.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() <= 0 {
		t.Errorf("retry demo error = %v, want a nonzero exit:\n%s", err, retryOutput)
	}
	if !strings.Contains(string(retryOutput), "create working directory") {
		t.Errorf("retry demo did not report a workdir collision:\n%s", retryOutput)
	}
	if afterRetry := snapshotTree(t, workdir); !reflect.DeepEqual(afterRetry, beforeRetry) {
		t.Error("retrying the demo changed the retained artifacts")
	}
	if err := run(t.TempDir(), evidenceBinary, root, filepath.Join(t.TempDir(), "bad-autogov-output"), false); err == nil {
		t.Fatal("expected a non-executable AutoGov path to fail without panicking")
	}
}

func retainedFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return files
}

func sortedArtifactNames(artifacts map[string]string) []string {
	names := make([]string, 0, len(artifacts))
	for name := range artifacts {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func TestRunCLIRejectsHelpAndPositionalArgumentsBeforeDemo(t *testing.T) {
	if err := runCLI([]string{"--help"}); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("help error = %v, want flag.ErrHelp", err)
	}
	if err := runCLI([]string{"unexpected"}); err == nil {
		t.Fatal("expected positional argument to fail")
	}
	if err := runCLI([]string{"--autogov", filepath.Join(t.TempDir(), "missing")}); err == nil {
		t.Fatal("expected a missing AutoGov binary to fail")
	}
	if err := runCLI([]string{"--autogov", "relative-autogov"}); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("relative explicit AutoGov path error = %v, want absolute-path rejection", err)
	}
}

func TestRunCLIDefaultAutoGovPathIsAbsolute(t *testing.T) {
	err := runCLI(nil)
	if err == nil {
		t.Fatal("default demo unexpectedly ran without its companion binary")
	}
	if strings.Contains(err.Error(), "must be an absolute path") {
		t.Fatalf("default AutoGov path remained relative: %v", err)
	}
}
