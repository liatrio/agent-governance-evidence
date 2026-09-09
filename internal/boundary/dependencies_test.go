package boundary

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const autoGovModulePath = "github.com/liatrio/autogov"

func TestStandaloneModuleRejectsAutoGovDependenciesAndReplacements(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	dependencies := goListDependencies(t, repoRoot, "./...")
	assertNoDependencyPrefix(t, dependencies, autoGovModulePath)

	if err := rejectAutoGovModuleMetadata(loadGoModMetadata(t, repoRoot)); err != nil {
		t.Fatal(err)
	}
	assertNoDependencyPrefix(t, goListModules(t, repoRoot), autoGovModulePath)
}

func TestModuleMetadataRejectsInactiveAutoGovAndAllReplacements(t *testing.T) {
	base := "module example.com/companion\n\ngo 1.26.6\n"
	cases := []struct {
		name, goMod string
	}{
		{
			name:  "unused AutoGov requirement",
			goMod: base + "\nrequire github.com/liatrio/autogov v1.4.0\n",
		},
		{
			name:  "block replacement targets AutoGov",
			goMod: base + "\nreplace (\n\texample.com/verifier => github.com/liatrio/autogov v1.4.0\n)\n",
		},
		{
			name:  "block replacement replaces AutoGov",
			goMod: base + "\nreplace (\n\tgithub.com/liatrio/autogov => example.com/verifier v1.0.0\n)\n",
		},
		{
			name:  "local replacement",
			goMod: base + "\nreplace example.com/verifier => ./verifier\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			goMod := filepath.Join(dir, "go.mod")
			if err := os.WriteFile(goMod, []byte(tc.goMod), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := rejectAutoGovModuleMetadata(loadGoModMetadata(t, dir)); err == nil {
				t.Fatal("unsafe module metadata was accepted")
			}
		})
	}
}

func goListDependencies(t *testing.T, repoRoot string, patterns ...string) []string {
	t.Helper()
	args := append([]string{"list", "-deps", "-test", "-f", "{{.ImportPath}}"}, patterns...)
	cmd := exec.Command("go", args...)
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list %v: %v\n%s", patterns, err, output)
	}
	return strings.Fields(string(output))
}

func goListModules(t *testing.T, repoRoot string) []string {
	t.Helper()
	cmd := exec.Command("go", "list", "-mod=readonly", "-m", "-f", "{{.Path}}", "all")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list modules: %v\n%s", err, output)
	}
	return strings.Fields(string(output))
}

type goModMetadata struct {
	Require []goModRequirement `json:"Require"`
	Replace []goModReplace     `json:"Replace"`
}

type goModRequirement struct {
	Path string `json:"Path"`
}

type goModReplace struct {
	Old goModModule `json:"Old"`
	New goModModule `json:"New"`
}

type goModModule struct {
	Path string `json:"Path"`
}

func loadGoModMetadata(t *testing.T, repoRoot string) goModMetadata {
	t.Helper()
	cmd := exec.Command("go", "mod", "edit", "-json")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("parse go.mod metadata: %v\n%s", err, output)
	}
	var metadata goModMetadata
	if err := json.Unmarshal(output, &metadata); err != nil {
		t.Fatalf("decode go.mod metadata: %v", err)
	}
	return metadata
}

func rejectAutoGovModuleMetadata(metadata goModMetadata) error {
	for _, requirement := range metadata.Require {
		if hasModulePrefix(requirement.Path, autoGovModulePath) {
			return fmt.Errorf("go.mod must not require AutoGov module %s", requirement.Path)
		}
	}
	for _, replacement := range metadata.Replace {
		if hasModulePrefix(replacement.Old.Path, autoGovModulePath) || hasModulePrefix(replacement.New.Path, autoGovModulePath) {
			return fmt.Errorf("go.mod must not replace AutoGov module %s => %s", replacement.Old.Path, replacement.New.Path)
		}
	}
	if len(metadata.Replace) != 0 {
		return fmt.Errorf("go.mod must not contain replacements")
	}
	return nil
}

func assertNoDependencyPrefix(t *testing.T, dependencies []string, forbidden string) {
	t.Helper()
	for _, dependency := range dependencies {
		if hasModulePrefix(dependency, forbidden) {
			t.Fatalf("dependency graph crosses extraction boundary through %s", dependency)
		}
	}
}

func hasModulePrefix(path, module string) bool {
	return path == module || strings.HasPrefix(path, module+"/")
}
